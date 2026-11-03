package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alexagent/internal/llm"
)

// upload posts a file as the client does and returns the status and answer.
func (c *client) upload(name string, data []byte) (int, map[string]any) {
	c.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.base+"/api/uploads", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Alexa", "1")
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

// A user (or a page that tricked their agent) links the server's data
// directory into the workspace. No file route reaches through it, with an
// absolute path, a relative one, or a file that doesn't exist yet
// (acceptance 1 of WP-S1, for the API's callers).
func TestFileRoutesRefuseLinksOut(t *testing.T) {
	e := newTestServer(t)
	alice := e.client(t)
	alice.signup("alice")
	ctx := context.Background()
	au, _ := e.st.GetUserByName(ctx, "alice")
	env, err := e.envs.Ensure(au.ID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(e.cfg.DataDir, "vault.key"))
	if err != nil || len(key) == 0 {
		t.Fatalf("no vault key to steal: %v", err)
	}
	if err := os.Symlink(e.cfg.DataDir, filepath.Join(env.Workspace, "d")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(e.cfg.DataDir, "planted.txt"), filepath.Join(env.Workspace, "new.txt")); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"d/vault.key", "./d/vault.key", "/workspace/d/vault.key",
		filepath.Join(env.Workspace, "d", "vault.key"),
		filepath.Join(env.Workspace, "d", "alexa.db"),
		"d/not-yet.txt", filepath.Join(env.Workspace, "d", "not-yet.txt"),
		"new.txt",
	}
	for _, p := range paths {
		code, body := alice.raw("GET", "/api/files/download?path="+url.QueryEscape(p), nil, false)
		if code == 200 || bytes.Contains(body, key) {
			t.Errorf("download %q = %d", p, code)
		}
		if code, _, _ := alice.do("POST", "/api/messages", map[string]any{"text": "look", "attachments": []any{map[string]any{"name": "x", "path": p}}}, true); code != 400 {
			t.Errorf("attaching %q = %d, want 400", p, code)
		}
		if strings.HasPrefix(p, "new.txt") {
			continue // deleting the link itself is fine
		}
		if code, _, _ := alice.do("DELETE", "/api/files?path="+url.QueryEscape(p), nil, true); code == 200 {
			t.Errorf("delete %q = %d", p, code)
		}
	}
	if b, err := os.ReadFile(filepath.Join(e.cfg.DataDir, "vault.key")); err != nil || !bytes.Equal(b, key) {
		t.Fatal("the vault key was deleted or changed")
	}
	// The workspace and its folders can't be deleted, however the path is
	// written (the old guard compared strings, so a trailing slash got by).
	for _, p := range []string{"", ".", "/", env.Workspace, env.Workspace + "/", "/workspace/", "uploads", "uploads/", "downloads"} {
		if code, _, _ := alice.do("DELETE", "/api/files?path="+url.QueryEscape(p), nil, true); code == 200 {
			t.Errorf("delete %q = %d", p, code)
		}
	}
	if _, err := os.Stat(env.Uploads); err != nil {
		t.Fatal("uploads/ is gone")
	}
	// The listing shows the links as they are and doesn't walk into them.
	_, _, list := alice.do("GET", "/api/files", nil, false)
	for _, f := range list {
		if p := f.(map[string]any)["path"].(string); strings.HasPrefix(p, "d/") {
			t.Errorf("the listing walked through the link: %s", p)
		}
	}
	// An upload never goes through a linked uploads/, and nothing is made
	// in the link's target (acceptance 2). The link is moved aside and a
	// real folder made, so uploads (and messages) keep working.
	other := t.TempDir()
	if err := os.RemoveAll(env.Uploads); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, env.Uploads); err != nil {
		t.Fatal(err)
	}
	code, m := alice.upload("note.txt", []byte("hi"))
	if code != 200 {
		t.Fatalf("upload after uploads/ was linked = %d %v", code, m)
	}
	if entries, _ := os.ReadDir(other); len(entries) != 0 {
		t.Errorf("the upload wrote into the link's target: %v", entries)
	}
	if fi, err := os.Lstat(env.Uploads); err != nil || !fi.IsDir() {
		t.Errorf("uploads/ is still a link: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(env.Workspace, m["path"].(string))); string(b) != "hi" {
		t.Errorf("uploaded file holds %q", b)
	}
	e.model.script(reply(1000, fn("say", map[string]any{"text": "Yes."})), reply(1000))
	if code, _, _ := alice.do("POST", "/api/messages", map[string]any{"text": "still here?"}, true); code != 200 {
		t.Errorf("sending after uploads/ was linked = %d", code)
	}
	if got := last(alice.wait("idle").Feed, "say"); got == nil || got.Text != "Yes." {
		t.Errorf("last say = %+v", got)
	}
}

func TestUploadKeepsNamesAndTypes(t *testing.T) {
	e := newTestServer(t)
	c := e.client(t)
	c.signup("alice")
	day := time.Now().Format("2006-01-02")
	for _, want := range []string{"租房合同.pdf", "租房合同-2.pdf"} {
		code, m := c.upload("租房合同.PDF", []byte("%PDF-1.4 lease"))
		if code != 200 || m["path"] != "uploads/"+day+"/"+want || m["name"] != "租房合同.PDF" || m["mime"] != "application/pdf" {
			t.Errorf("upload = %d %v, want uploads/%s/%s", code, m, day, want)
		}
	}
	for in, want := range map[string]string{
		"Résumé – final.docx": "Résumé – final.docx",
		"..hidden.txt":        "hidden.txt",
		"a:b*c?.txt":          "abc.txt",
		"😀.png":               "😀.png",
		"   .txt":             "file.txt",
	} {
		_, m := c.upload(in, []byte("text"))
		if got, _ := m["path"].(string); got != "uploads/"+day+"/"+want {
			t.Errorf("upload %q stored as %q, want %q", in, got, want)
		}
	}
	// The download keeps the name, for any browser.
	req, _ := http.NewRequest("GET", c.base+"/api/files/download?path="+url.QueryEscape("uploads/"+day+"/租房合同.pdf"), nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "%PDF-1.4 lease" {
		t.Fatalf("download = %d %q", resp.StatusCode, body)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, `filename*=UTF-8''%E7%A7%9F%E6%88%BF%E5%90%88%E5%90%8C.pdf`) || !strings.Contains(cd, `filename="`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	// A picture's type comes from its content, whatever the browser said.
	_, m := c.upload("photo.png", jpegFile(t))
	if m["mime"] != "image/jpeg" {
		t.Errorf("a JPEG named .png has type %v", m["mime"])
	}
}

func TestUploadTooLarge(t *testing.T) {
	e := newTestServer(t)
	c := e.client(t)
	c.signup("alice")
	defer func(n int64) { maxUpload = n }(maxUpload)
	maxUpload = 1 << 20
	code, m := c.upload("big.bin", bytes.Repeat([]byte("x"), 2<<20))
	if code != 413 || !strings.Contains(m["error"].(string), "too large") {
		t.Fatalf("too-large upload = %d %v", code, m)
	}
}

func jpegFile(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 40, 30)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The PoisonAttachment repro (review L3), end to end: an SVG, a HEIC and a
// cut-off JPEG are attached. The model, like the real one, refuses any
// picture that isn't a JPEG it can decode; none is ever sent, so every
// turn works and the conversation carries on.
func TestPoisonAttachmentsNeverReachTheModel(t *testing.T) {
	e := newTestServer(t)
	c := e.client(t)
	c.signup("alice")
	jpg := jpegFile(t)
	var atts []any
	for name, data := range map[string][]byte{
		"logo.svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"/>`),
		"photo.heic": append([]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"), bytes.Repeat([]byte{1}, 100)...),
		"cut.jpg":    jpg[:len(jpg)/2],
		"fine.jpg":   jpg,
	} {
		code, m := c.upload(name, data)
		if code != 200 {
			t.Fatalf("upload %s = %d %v", name, code, m)
		}
		if note, _ := m["note"].(string); (note == "") != (name == "fine.jpg") {
			t.Errorf("%s: note %q", name, note)
		}
		atts = append(atts, m)
	}
	pictures := func(req *llm.Request) int {
		n := 0
		for _, raw := range req.Input {
			var msg struct {
				Content []map[string]any `json:"content"`
			}
			_ = json.Unmarshal(raw, &msg)
			for _, part := range msg.Content {
				if part["type"] != "input_image" {
					continue
				}
				n++
				data, ok := strings.CutPrefix(part["image_url"].(string), "data:image/jpeg;base64,")
				b, _ := base64.StdEncoding.DecodeString(data)
				if _, err := jpeg.Decode(bytes.NewReader(b)); !ok || err != nil {
					t.Errorf("the model was sent a picture it would refuse: %.60s", part["image_url"])
				}
			}
		}
		return n
	}
	look := func(wantText ...string) turn {
		return func(_ context.Context, req *llm.Request) ([]any, int64) {
			if n := pictures(req); n != 1 {
				t.Errorf("%d pictures sent, want only fine.jpg", n)
			}
			in := inputText(req)
			for _, w := range wantText {
				if !strings.Contains(in, w) {
					t.Errorf("model input lacks %q", w)
				}
			}
			return []any{fn("say", map[string]any{"text": "Got it."})}, 1000
		}
	}
	e.model.script(look("SVG drawing", "HEIC", "damaged"), reply(1000), look(), reply(1000))
	if code, m, _ := c.do("POST", "/api/messages", map[string]any{"text": "what are these?", "attachments": atts}, true); code != 200 {
		t.Fatalf("send: %d %v", code, m)
	}
	c.wait("idle")
	c.send("hello?")
	s := c.wait("idle")
	for _, en := range s.Feed {
		if en.Kind == "error" {
			t.Errorf("error in the feed: %s", en.Text)
		}
	}
	if got := last(s.Feed, "say"); got == nil || got.Text != "Got it." {
		t.Errorf("last say = %+v", got)
	}
}
