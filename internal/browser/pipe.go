package browser

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

// Chrome's DevTools protocol goes over a pair of pipes
// (--remote-debugging-pipe), never a TCP port. Chrome doesn't authenticate
// its debugging port, and every UID on the machine shares loopback: any
// local process (another user's shell in the container, a fetch the server
// makes for the agent) could drive any user's Chrome through it, reading
// their tabs and cookies, running script in their pages, or opening file://
// and chrome:// pages as that user, past navTarget. chromedp only speaks
// websocket, so the server bridges the two (bridge).

// chrome is a running Chrome.
type chrome struct {
	cmd  *exec.Cmd
	url  string        // the bridge chromedp connects to, once
	done chan struct{} // closed once Chrome has exited
	out  *lastOutput
}

// startChrome starts Chrome with its debugging pipe on descriptors 3 (what
// it reads) and 4 (what it writes), and the bridge chromedp connects to.
func startChrome(path string, args []string, env UserEnv) (*chrome, error) {
	cmdR, cmdW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	resR, resW, err := os.Pipe()
	if err != nil {
		cmdR.Close()
		cmdW.Close()
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		cmdR.Close()
		cmdW.Close()
		resR.Close()
		resW.Close()
		return nil, err
	}
	cmd := chromeCmd(path, append([]string{"--remote-debugging-pipe"}, args...), env)
	cmd.ExtraFiles = []*os.File{cmdR, resW}
	// A file, not a writer: exec would otherwise wait in Wait until every
	// helper process Chrome started had let go of its output too.
	cmd.Stdout, cmd.Stderr = outW, outW
	err = cmd.Start()
	cmdR.Close() // Chrome has its own copies now
	resW.Close()
	outW.Close()
	if err != nil {
		cmdW.Close()
		resR.Close()
		outR.Close()
		return nil, err
	}
	c := &chrome{cmd: cmd, done: make(chan struct{}), out: &lastOutput{}}
	go func() {
		_, _ = io.Copy(c.out, outR)
		outR.Close()
	}()
	go func() {
		_ = cmd.Wait()
		close(c.done)
	}()
	if c.url, err = bridge(cmdW, resR, c.done); err != nil {
		c.stop()
		return nil, err
	}
	return c, nil
}

// chromeCmd is the command that runs Chrome for env: with nothing of the
// server's environment but chromeEnv's list, under the user's UID if they
// have one, and (on Linux) killed if the server dies.
func chromeCmd(path string, args []string, env UserEnv) *exec.Cmd {
	cmd := exec.Command(path, args...)
	cmd.Env = chromeEnv(env)
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	dieWithParent(cmd.SysProcAttr)
	if env.UID > 0 {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(env.UID), Gid: uint32(env.GID)}
	}
	return cmd
}

// stop kills Chrome and waits a little for it to go.
func (c *chrome) stop() {
	_ = c.cmd.Process.Kill()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
	}
}

// exited reports whether Chrome has exited, and says how it ended if it
// did, with the end of what it printed.
func (c *chrome) exited() string {
	select {
	case <-c.done:
	default:
		return ""
	}
	msg := "chrome exited"
	if s := c.cmd.ProcessState; s != nil {
		msg = "chrome exited: " + s.String()
	}
	if out := strings.TrimSpace(c.out.String()); out != "" {
		msg += "\n" + out
	}
	return msg
}

// bridge serves Chrome's debugging pipe as a websocket on loopback, for one
// connection: chromedp's. It's at a random path, so nothing else that
// connects (while the listener is open) can use it, and the listener
// closes as soon as chromedp has connected, or Chrome has exited.
func bridge(toChrome io.WriteCloser, fromChrome io.ReadCloser, exited <-chan struct{}) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		toChrome.Close()
		fromChrome.Close()
		return "", err
	}
	secret := make([]byte, 16)
	_, _ = rand.Read(secret)
	path := "/devtools/browser/" + hex.EncodeToString(secret)
	var once sync.Once
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		taken := true
		if r.URL.Path == path {
			once.Do(func() { taken = false })
		}
		if taken {
			http.NotFound(w, r)
			return
		}
		ln.Close()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			toChrome.Close()
			fromChrome.Close()
			return
		}
		pump(conn, toChrome, fromChrome)
	})}
	go func() {
		<-exited
		ln.Close()
	}()
	go func() { _ = srv.Serve(ln) }()
	return "ws://" + ln.Addr().String() + path, nil
}

// pump carries messages between the websocket and Chrome's pipe, where
// each message ends in a NUL, until either side closes. Closing the pipe to
// Chrome makes it quit.
func pump(conn *websocket.Conn, toChrome io.WriteCloser, fromChrome io.ReadCloser) {
	conn.SetReadLimit(-1) // a command can carry a whole script
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		br := bufio.NewReaderSize(fromChrome, 1<<16)
		for {
			msg, err := br.ReadBytes(0)
			if err != nil {
				return
			}
			if conn.Write(ctx, websocket.MessageText, msg[:len(msg)-1]) != nil {
				return
			}
		}
	}()
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			break
		}
		if _, err := toChrome.Write(append(msg, 0)); err != nil {
			break
		}
	}
	cancel()
	_ = conn.CloseNow()
	toChrome.Close()
	fromChrome.Close()
}

// chromePath is the Chrome to run: the one configured, or else the first
// found where chromedp looks for one.
func chromePath(p string) string {
	if p == "" {
		names := []string{"headless_shell", "headless-shell", "chromium", "chromium-browser", "google-chrome", "google-chrome-stable",
			"google-chrome-beta", "google-chrome-unstable", "/usr/bin/google-chrome", "/usr/local/bin/chrome", "/snap/bin/chromium", "chrome"}
		if runtime.GOOS == "darwin" {
			names = []string{"/Applications/Chromium.app/Contents/MacOS/Chromium", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
		}
		for _, n := range names {
			if full, err := exec.LookPath(n); err == nil {
				return full
			}
		}
		return "google-chrome"
	}
	if full, err := exec.LookPath(p); err == nil {
		return full
	}
	return p
}

// lastOutput keeps the end of what Chrome prints, for the error when it
// won't start.
type lastOutput struct {
	mu  sync.Mutex
	buf []byte
}

const keepOutput = 4096

func (o *lastOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.buf = append(o.buf, p...)
	if len(o.buf) > keepOutput {
		o.buf = append(o.buf[:0], o.buf[len(o.buf)-keepOutput:]...)
	}
	return len(p), nil
}

func (o *lastOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return string(o.buf)
}

// errLaunch adds how Chrome ended, if it did, to an error starting it.
func (c *chrome) errLaunch(err error) error {
	if why := c.exited(); why != "" {
		return fmt.Errorf("launch chrome: %w (%s)", err, why)
	}
	return fmt.Errorf("launch chrome: %w", err)
}
