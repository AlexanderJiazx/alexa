package agent

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // decoders for read_file's image view
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"alexagent/internal/userenv"
)

// read_file shows workspace images and PDF pages to the model as pictures,
// so it can check an image it generated, or read a scan, chart or form.

const maxViewSide = 1600 // longest side of an image shown to the model

// findTool looks for a program where run_shell would.
func findTool(name string) string {
	for _, d := range filepath.SplitList(shellPath) {
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// asUser runs cmd as the user's own UID, as run_shell does: the files it
// reads come from the user or the web.
func asUser(cmd *exec.Cmd, env *userenv.Env) {
	cmd.Env = []string{"HOME=" + env.Home, "PATH=" + shellPath, "LANG=C.UTF-8"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if env.UID > 0 {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(env.UID), Gid: uint32(env.GID)}
	}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

// toJPEG decodes an image and re-encodes it as a JPEG no larger than
// maxViewSide on its long side, flattened onto white.
func toJPEG(data []byte) ([]byte, int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	if cfg.Width*cfg.Height > 80_000_000 {
		return nil, 0, 0, fmt.Errorf("too large to show (%d×%d)", cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if long := max(w, h); long > maxViewSide {
		w, h = max(w*maxViewSide/long, 1), max(h*maxViewSide/long, 1)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 80}); err != nil {
		return nil, 0, 0, err
	}
	return out.Bytes(), b.Dx(), b.Dy(), nil
}

// readBinary handles read_file for images and PDFs; ok is false for other
// binary files.
func readBinary(tc *toolCtx, full string, data []byte, page, offset int) (*toolResult, bool) {
	rel := tc.run.env.Rel(full)
	switch ct := http.DetectContentType(data); {
	case strings.HasPrefix(ct, "image/"):
		jpg, w, h, err := toJPEG(data)
		if err != nil {
			return &toolResult{Text: fmt.Sprintf("%s is an image (%s, %d bytes) that couldn't be shown: %v", rel, ct, len(data), err)}, true
		}
		return &toolResult{Text: fmt.Sprintf("Showing image %s (%d×%d).", rel, w, h), Image: jpg}, true
	case ct == "application/pdf":
		return readPDF(tc, full, rel, page, offset), true
	}
	return nil, false
}

func readPDF(tc *toolCtx, full, rel string, page, offset int) *toolResult {
	env := tc.run.env
	if findTool("pdftotext") == "" {
		return &toolResult{Text: fmt.Sprintf("%s is a PDF, and there are no PDF tools here to read it.", rel)}
	}
	ctx, cancel := context.WithTimeout(tc.ctx, 90*time.Second)
	defer cancel()
	run := func(name string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, findTool(name), args...)
		asUser(cmd, env)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil && stderr.Len() > 0 {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return out, err
	}
	pages := 0
	if out, err := run("pdfinfo", full); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if v, ok := strings.CutPrefix(line, "Pages:"); ok {
				pages, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
	}
	if page > 0 {
		if pages > 0 && page > pages {
			return &toolResult{Text: fmt.Sprintf("%s has %d pages.", rel, pages)}
		}
		p := strconv.Itoa(page)
		text, _ := run("pdftotext", "-layout", "-f", p, "-l", p, full, "-")
		res := &toolResult{Kind: "long"}
		if findTool("pdftoppm") != "" {
			if img, err := run("pdftoppm", "-f", p, "-l", p, "-r", "110", "-jpeg", "-singlefile", full); err == nil {
				if jpg, _, _, err := toJPEG(img); err == nil {
					res.Image = jpg
				}
			}
		}
		head := fmt.Sprintf("Page %d of %s", page, rel)
		if pages > 0 {
			head = fmt.Sprintf("Page %d of %d of %s", page, pages, rel)
		}
		if res.Image != nil {
			head += " is shown below"
		}
		res.Text = head + ". Its text:\n\n" + clipRunes(collapseBlankLines(string(text)), 12000)
		return res
	}
	out, err := run("pdftotext", "-layout", full, "-")
	if err != nil {
		return &toolResult{Text: fmt.Sprintf("Couldn't read %s: %v", rel, err)}
	}
	text := collapseBlankLines(string(out))
	head := fmt.Sprintf("%s is a PDF", rel)
	if pages > 0 {
		head += fmt.Sprintf(" of %d pages", pages)
	}
	head += ". Its text follows; pass page=N to see a page as an image (for scans, charts and forms)."
	if letters(text) < 40*max(pages, 1) {
		head += " It has little or no text layer, so it's probably scanned: look at its pages instead."
	}
	if offset > len(text) {
		offset = len(text)
	}
	s, more := text[offset:], ""
	if len(s) > 30000 {
		s = s[:30000]
		more = fmt.Sprintf("\n[…truncated; continue with offset=%d]", offset+30000)
	}
	return &toolResult{Text: head + "\n\n" + s + more, Kind: "long"}
}

func letters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}

func collapseBlankLines(s string) string {
	var out []string
	blank := 0
	for _, l := range strings.Split(strings.ReplaceAll(s, "\f", "\n"), "\n") {
		l = strings.TrimRight(l, " \t")
		if l == "" {
			if blank++; blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "\n[…]"
	}
	return s
}
