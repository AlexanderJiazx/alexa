package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// fileTools work on the user's private workspace.
func fileTools() []*tool {
	return []*tool{
		{
			Name:        "list_files",
			Description: "List files in the workspace (or a sub-directory).",
			Params:      map[string]any{"path": p("string", "Directory (default: workspace root)")},
			Run:         listFiles,
		},
		{
			Name: "read_file",
			Description: "Read a file from the workspace. Text comes back as text (long files are paged: pass offset). Images (PNG, JPEG, GIF, WebP) are shown to you, so you can look at them, e.g. to check an image you generated. " +
				"PDFs come back as text; pass page to see that page as an image (scans, charts, forms).",
			Params:   map[string]any{"path": p("string", "File path"), "offset": p("integer", "Character offset"), "page": p("integer", "For a PDF: the page to look at, from 1")},
			Required: []string{"path"},
			Run:      readFile,
		},
		{
			Name:        "write_file",
			Description: "Create or overwrite a text file in the workspace (reports, notes, CSV, markdown, HTML…).",
			Params:      map[string]any{"path": p("string", "File path"), "content": p("string", "File content"), "append": p("boolean", "Append instead of overwrite")},
			Required:    []string{"path", "content"},
			Run:         writeFile,
		},
	}
}

// shellTool runs commands in the workspace, when the shell is enabled.
func shellTool() *tool {
	return &tool{
		Name:        "run_shell",
		Description: "Run a shell command (bash) in the workspace directory. " + shellTools() + " Use for calculations, data processing, charts, reading and converting files, and generating documents. Not for browsing.",
		Params:      map[string]any{"command": p("string", "The bash command"), "timeout_seconds": p("integer", "Timeout (default 120, max 900)")},
		Required:    []string{"command"},
		Run:         runShell,
	}
}

// imageTool generates images, when an image model is configured.
func imageTool() *tool {
	return &tool{
		Name:        "generate_image",
		Description: "Generate an image from a text prompt and save it to the workspace.",
		Params:      map[string]any{"prompt": p("string", "Detailed description"), "path": p("string", "Workspace path, e.g. images/poster.png")},
		Required:    []string{"prompt", "path"},
		Run:         generateImage,
	}
}

func listFiles(tc *toolCtx, a map[string]any) (*toolResult, error) {
	env := tc.run.env
	dir := env.Clean(str(a, "path"))
	if _, err := env.Stat(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	// Walking the workspace root never leaves it: a link out shows as an
	// entry and isn't followed.
	root, err := env.OpenRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var lines []string
	count := 0
	err = fs.WalkDir(root.FS(), filepath.ToSlash(dir), func(rel string, d fs.DirEntry, err error) error {
		if err != nil || rel == filepath.ToSlash(dir) {
			return nil
		}
		depth := strings.Count(rel, "/") + 1
		if dir != "." {
			depth = strings.Count(strings.TrimPrefix(rel, filepath.ToSlash(dir)), "/")
		}
		if d.IsDir() {
			if depth > 3 || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			lines = append(lines, rel+"/")
			return nil
		}
		count++
		if count > 300 {
			return filepath.SkipAll
		}
		info, _ := d.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		lines = append(lines, fmt.Sprintf("%s (%d bytes)", rel, size))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return &toolResult{Text: "(empty)"}, nil
	}
	return &toolResult{Text: strings.Join(lines, "\n")}, nil
}

// maxReadFile is the most read_file loads at once; bigger files are for
// run_shell.
const maxReadFile = 100 << 20

func readFile(tc *toolCtx, a map[string]any) (*toolResult, error) {
	env := tc.run.env
	full := filepath.Join(env.Workspace, env.Clean(str(a, "path")))
	data, err := env.ReadFile(str(a, "path"), maxReadFile)
	if err != nil {
		return nil, err
	}
	if res, ok := readBinary(tc, full, data, int(num(a, "page", 0)), int(num(a, "offset", 0))); ok {
		return res, nil
	}
	if !isText(data) {
		return &toolResult{Text: fmt.Sprintf("%s is a binary file (%d bytes, %s). Use run_shell to process it.", str(a, "path"), len(data), mime.TypeByExtension(filepath.Ext(full)))}, nil
	}
	s, more := pageOf(string(data), int(num(a, "offset", 0)), readPage)
	return &toolResult{Text: s + more, Kind: "long"}, nil
}

// readPage is how much of a long text read_file shows at once, in bytes.
const readPage = 30000

// pageOf is the part of s that starts at byte offset off (clamped to s),
// at most n bytes long, with the note that says where the next part
// starts when there is more. Both ends fall on whole characters: an offset
// inside one starts at that character (BH-9).
func pageOf(s string, off, n int) (page, more string) {
	off = min(max(off, 0), len(s))
	for off > 0 && off < len(s) && !utf8.RuneStart(s[off]) {
		off--
	}
	end := len(s)
	if end-off > n {
		end = off + len(validPrefix([]byte(s[off:off+n])))
		if end == off { // n is less than one character
			_, size := utf8.DecodeRuneInString(s[off:])
			end = off + size
		}
		more = fmt.Sprintf("\n[…truncated; continue with offset=%d]", end)
	}
	return s[off:end], more
}

// cutText is s cut to at most n bytes on a whole character.
func cutText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return string(validPrefix([]byte(s[:n])))
}

func isText(b []byte) bool {
	n := min(len(b), 4096)
	return !bytes.ContainsRune(b[:n], 0)
}

func writeFile(tc *toolCtx, a map[string]any) (*toolResult, error) {
	env := tc.run.env
	flag := os.O_TRUNC
	if boolean(a, "append", false) {
		flag = os.O_APPEND
	}
	f, err := env.Create(str(a, "path"), flag)
	if err != nil {
		return nil, err
	}
	_, err = f.WriteString(str(a, "content"))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	return &toolResult{Text: "Wrote " + env.Clean(str(a, "path"))}, nil
}

func runShell(tc *toolCtx, a map[string]any) (*toolResult, error) {
	timeout := time.Duration(min(max(num(a, "timeout_seconds", 120), 1), 900)) * time.Second
	ctx, cancel := context.WithTimeout(tc.ctx, timeout)
	defer cancel()
	env := tc.run.env
	// /tmp is shared by every user's commands; temporary files go to a
	// private directory instead.
	tmp := filepath.Join(env.Home, "tmp")
	if err := env.MkdirAll(tmp); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "bash", "-c", str(a, "command"))
	cmd.Dir = env.Workspace
	cmd.Env = []string{
		"HOME=" + env.Home,
		"TMPDIR=" + tmp,
		"PATH=" + shellPath,
		"LANG=C.UTF-8",
		"TZ=" + tc.run.user.Timezone,
		"WORKSPACE=" + env.Workspace,
		"MPLBACKEND=Agg",
	}
	// Commands reach the network the same way the server does.
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		if v, ok := os.LookupEnv(k); ok {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	// The command runs in a process group of its own, which the timeout
	// kills whole (BH-1).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if env.UID > 0 {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(env.UID), Gid: uint32(env.GID)}
	}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	// Something the command left running ("start a server &") holds on to
	// its output: once the command is over, or killed at the timeout, the
	// output is read for shellWaitDelay more and then cut off, so the call
	// returns then and not when that process lets go.
	cmd.WaitDelay = shellWaitDelay
	out := &headTail{max: shellKeep}
	cmd.Stdout = out
	cmd.Stderr = cmd.Stdout
	err := cmd.Run()
	if cmd.Process != nil {
		// What the command left running in its group ends with it. A
		// process that left the group (setsid) is out of reach, but no
		// longer holds the call up.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	text := out.String()
	code := 0
	if ps := cmd.ProcessState; ps != nil {
		code = ps.ExitCode()
	}
	var ee *exec.ExitError
	switch {
	case cmd.ProcessState != nil && cmd.ProcessState.Exited():
		// It finished. An error here only says it left something holding
		// its output (exec.ErrWaitDelay), or that the timeout came just as
		// it ended.
	case ctx.Err() == context.DeadlineExceeded:
		text += fmt.Sprintf("\n[timed out after %s]", timeout)
		code = -1
	case err == nil, errors.As(err, &ee):
		// Killed by a signal: the run was stopped, say.
	default:
		return nil, err
	}
	return &toolResult{Text: fmt.Sprintf("exit code %d\n%s", code, text), Kind: "long"}, nil
}

// shellWaitDelay is how long run_shell goes on reading a command's output
// once it has exited or was killed, for what it left running.
const shellWaitDelay = time.Second

// shellKeep is how much of a command's output run_shell shows from its
// start and from its end.
const shellKeep = 15000

// headTail keeps the first and the last max bytes written to it (M7): a
// script that prints megabytes of progress and then a traceback shows the
// traceback, with a marker saying how much was left out between.
type headTail struct {
	max     int
	head    []byte
	tail    []byte // a ring once full; start is its oldest byte
	start   int
	dropped int64 // bytes that fell out between head and tail
}

func (h *headTail) Write(p []byte) (int, error) {
	n := len(p)
	if room := h.max - len(h.head); room > 0 {
		k := min(room, len(p))
		h.head = append(h.head, p[:k]...)
		p = p[k:]
	}
	for len(p) > 0 {
		if len(h.tail) < h.max {
			k := min(h.max-len(h.tail), len(p))
			h.tail = append(h.tail, p[:k]...)
			p = p[k:]
			continue
		}
		k := min(h.max-h.start, len(p))
		h.dropped += int64(k)
		copy(h.tail[h.start:], p[:k])
		h.start = (h.start + k) % h.max
		p = p[k:]
	}
	return n, nil
}

// String is the output kept, cut on whole characters around the marker.
func (h *headTail) String() string {
	tail := append(append([]byte{}, h.tail[h.start:]...), h.tail[:h.start]...)
	if h.dropped == 0 {
		return string(h.head) + string(tail)
	}
	head := validPrefix(h.head)
	cut := len(tail)
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	omitted := h.dropped + int64(len(h.head)-len(head)) + int64(cut-len(tail))
	return fmt.Sprintf("%s\n[… %d bytes omitted …]\n%s", head, omitted, tail)
}

// validPrefix drops a character cut in two at the end of b.
func validPrefix(b []byte) []byte {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return b[:i]
			}
			break
		}
	}
	return b
}

const shellPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/homebrew/bin"

// shellTools says what run_shell has to work with, so the model doesn't
// plan around tools that aren't there. It's checked once at startup: the
// image (or the machine) decides what's installed.
func shellTools() string {
	var have []string
	if py := findTool("python3"); py != "" {
		// Import names, and what to call them.
		mods := [][2]string{{"matplotlib", "matplotlib"}, {"pandas", "pandas"}, {"numpy", "numpy"}, {"openpyxl", "openpyxl"},
			{"docx", "python-docx"}, {"pypdf", "pypdf"}, {"PIL", "Pillow"}, {"bs4", "BeautifulSoup"}, {"requests", "requests"}}
		args := []string{"-c", "import importlib.util as u, sys\nprint(' '.join(m for m in sys.argv[1:] if u.find_spec(m)))"}
		for _, m := range mods {
			args = append(args, m[0])
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(ctx, py, args...).Output()
		found := map[string]bool{}
		for _, m := range strings.Fields(string(out)) {
			found[m] = true
		}
		var libs []string
		for _, m := range mods {
			if found[m[0]] {
				libs = append(libs, m[1])
			}
		}
		if len(libs) > 0 {
			have = append(have, "Python 3 (with "+strings.Join(libs, ", ")+")")
		} else {
			have = append(have, "Python 3 (standard library only)")
		}
	}
	for _, name := range []string{"ffmpeg", "pdftotext", "pdftoppm", "tesseract", "jq", "curl", "unzip", "zip"} {
		if findTool(name) != "" {
			have = append(have, name)
		}
	}
	if len(have) == 0 {
		return "Only basic shell utilities are installed."
	}
	return "Installed: " + strings.Join(have, ", ") + "."
}

type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	q := p
	if len(q) > l.n {
		q = q[:l.n]
	}
	l.n -= len(q)
	_, err := l.w.Write(q)
	return len(p), err
}

func generateImage(tc *toolCtx, a map[string]any) (*toolResult, error) {
	env := tc.run.env
	rel := env.Clean(str(a, "path"))
	// Refuse a bad path before paying for the image; the write below checks
	// again, through the workspace root, once the final name is known.
	if rel == "." {
		return nil, errors.New("give the image a file name, e.g. images/poster.png")
	}
	if _, err := env.Resolve(rel); err != nil {
		return nil, err
	}
	img, err := tc.rt.generateImage(tc.ctx, str(a, "prompt"))
	if err != nil {
		return nil, err
	}
	// Name the file after what the model actually returned (often WebP).
	exts := map[string]string{"image/webp": ".webp", "image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif"}
	if ext, ok := exts[http.DetectContentType(img)]; ok && !strings.EqualFold(filepath.Ext(rel), ext) && !(ext == ".jpg" && strings.EqualFold(filepath.Ext(rel), ".jpeg")) {
		rel = strings.TrimSuffix(rel, filepath.Ext(rel)) + ext
	}
	f, err := env.Create(rel, os.O_TRUNC)
	if err != nil {
		return nil, err
	}
	_, err = f.Write(img)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	return &toolResult{Text: "Saved image to " + rel + ". To show it, attach it to a report (present_report files)."}, nil
}

func (rt *Runtime) generateImage(ctx context.Context, prompt string) ([]byte, error) {
	body := marshal(map[string]any{"model": rt.cfg.ImageModel, "prompt": prompt, "n": 1})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, rt.cfg.BaseURL+"/images/generations", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+rt.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("image API %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil || len(out.Data) == 0 {
		return nil, fmt.Errorf("unexpected image API response")
	}
	if out.Data[0].B64 != "" {
		return base64.StdEncoding.DecodeString(out.Data[0].B64)
	}
	r2, err := http.Get(out.Data[0].URL)
	if err != nil {
		return nil, err
	}
	defer r2.Body.Close()
	return io.ReadAll(io.LimitReader(r2.Body, 64<<20))
}
