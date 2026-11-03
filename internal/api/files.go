package api

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"alexagent/internal/agent"
	"alexagent/internal/store"
	"alexagent/internal/userenv"
)

// Every handler here reaches the user's files through their workspace root
// (userenv's Open, Create, Remove), so no path, link or file that doesn't
// exist yet can lead the server, which runs as root in the container, to
// anything outside it.

// maxUpload is the largest file a user can upload (a variable for tests).
var maxUpload int64 = 100 << 20

func (s *Server) checkAttachments(u *store.User, list []agent.Attachment) ([]agent.Attachment, error) {
	env := s.envs.For(u.ID)
	var out []agent.Attachment
	for _, a := range list {
		info, err := env.Stat(a.Path)
		if errors.Is(err, userenv.ErrOutside) {
			return nil, err
		}
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("attachment %s not found", a.Path)
		}
		a.Path = env.Clean(a.Path)
		a.Size = info.Size()
		// The type comes from the file, not from what the client says: the
		// model is sent pictures and PDFs as such.
		a.Mime = agent.SniffMime(fileHead(env, a.Path), a.Path, a.Mime)
		out = append(out, a)
	}
	return out, nil
}

func fileHead(env *userenv.Env, p string) []byte {
	f, err := env.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	return head[:n]
}

// uploaded is the answer to an upload: the attachment, and a note when a
// picture can't be shown to Alexa (an SVG or a HEIC photo, say), for the
// person attaching it.
type uploaded struct {
	agent.Attachment
	Note string `json:"note,omitempty"`
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	env, err := s.envs.Ensure(u.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		if tooLarge(err) {
			writeErr(w, 413, uploadTooLarge())
			return
		}
		writeErr(w, 400, "missing file")
		return
	}
	defer f.Close()
	base, ext := fileName(hdr.Filename)
	dir := filepath.Join("uploads", time.Now().Format("2006-01-02"))
	// A file keeps its name, which a site Alexa uploads it to sees too; a
	// number is added when that day already has a file by that name.
	var rel string
	var out *os.File
	for i := 1; ; i++ {
		rel = filepath.Join(dir, base+ext)
		if i > 1 {
			rel = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		}
		out, err = env.Create(rel, os.O_EXCL)
		if !errors.Is(err, fs.ErrExist) || i == 1000 {
			break
		}
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	n, err := io.Copy(out, f)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = env.Remove(rel)
		if tooLarge(err) {
			writeErr(w, 413, uploadTooLarge())
			return
		}
		writeErr(w, 400, "upload failed")
		return
	}
	// The type is settled from the content, and a picture is turned into
	// the JPEG the model will be shown; the original stays as it was.
	att, note := agent.PrepareAttachment(r.Context(), env, agent.Attachment{Name: shownName(hdr.Filename, base+ext), Path: rel, Mime: hdr.Header.Get("Content-Type"), Size: n})
	s.changed(r, "files")
	writeJSON(w, uploaded{Attachment: att, Note: note})
}

// uploadTooLarge is shown after "Couldn’t upload <name>: ".
func uploadTooLarge() string {
	return fmt.Sprintf("that file is too large (the limit is %d MB)", maxUpload>>20)
}

func tooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe) || (err != nil && strings.Contains(err.Error(), "request body too large"))
}

// fileName makes an uploaded file's name safe to store, keeping letters in
// any script (租房合同.pdf stays 租房合同.pdf): only path separators, control
// characters and the few characters file systems reserve are taken out.
func fileName(name string) (base, ext string) {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	raw := filepath.Ext(name)
	base = cleanName(strings.TrimSuffix(name, raw), 80)
	if ext = cleanName(strings.ToLower(strings.TrimPrefix(raw, ".")), 12); ext != "" {
		ext = "." + ext
	}
	if base == "" {
		base = "file"
	}
	return base, ext
}

// shownName is the name an upload goes by in the conversation: the one it
// had on the person's device, in any script and case, on one line.
func shownName(name, stored string) string {
	if n := cleanName(filepath.Base(strings.ReplaceAll(name, `\`, "/")), 120); n != "" {
		return n
	}
	return stored
}

func cleanName(s string, maxRunes int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == utf8.RuneError, unicode.IsControl(r), strings.ContainsRune(`/\:*?"<>|`, r):
			return -1
		case unicode.IsSpace(r):
			return ' '
		}
		return r
	}, s)
	// Leading dots would hide the file from the listing.
	s = strings.TrimLeft(strings.TrimSpace(s), ".")
	if r := []rune(s); len(r) > maxRunes {
		s = strings.TrimSpace(string(r[:maxRunes]))
	}
	return s
}

// contentDisposition names a download so browsers keep a name in any script
// (filename*), with a plain ASCII one for the rest.
func contentDisposition(disp, name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disp, ascii, url.PathEscape(name))
}

type fileInfo struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
	IsDir   bool   `json:"isDir"`
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	env, err := s.envs.Ensure(userFrom(r).ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	root, err := env.OpenRoot()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer root.Close()
	var out []fileInfo
	// The walk stays inside the workspace root; a link shows as itself.
	_ = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(out) > 2000 {
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, fileInfo{Path: filepath.FromSlash(p), Size: info.Size(), ModTime: info.ModTime().UnixMilli(), IsDir: d.IsDir()})
		return nil
	})
	if out == nil {
		out = []fileInfo{}
	}
	writeJSON(w, out)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	env := s.envs.For(userFrom(r).ID)
	p := r.URL.Query().Get("path")
	f, err := env.Open(p)
	if errors.Is(err, userenv.ErrOutside) {
		writeErr(w, 400, err.Error())
		return
	}
	if err != nil {
		writeErr(w, 404, "file not found")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeErr(w, 404, "file not found")
		return
	}
	disp := "attachment"
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Query().Get("inline") == "1" {
		disp = "inline"
		// Never render user content as active HTML on our origin.
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	name := filepath.Base(env.Clean(p))
	w.Header().Set("Content-Disposition", contentDisposition(disp, name))
	// Served from the file opened through the workspace root, never by path.
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	env := s.envs.For(userFrom(r).ID)
	p := r.URL.Query().Get("path")
	// Deleting what's already gone is fine.
	if err := env.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeErr(w, 400, err.Error())
		return
	}
	agent.ForgetAttachment(env, p)
	s.changed(r, "files")
	writeJSON(w, map[string]bool{"ok": true})
}
