package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	_ "golang.org/x/image/bmp" // more decoders, so these become JPEGs too
	_ "golang.org/x/image/tiff"

	"alexagent/internal/llm"
	"alexagent/internal/userenv"
)

// Attachments reach the model as a picture or a document only in a form
// it's sure to take: every picture becomes a JPEG (toJPEG) once, when it's
// uploaded or first replayed, and that JPEG is what every later turn
// sends. The original stays as it was, for download and for the tools. A
// picture that can't be converted (an SVG drawing, a HEIC photo with no
// converter installed, a damaged file) is described in text instead, so
// one bad file can't make every later turn fail. If the model still
// refuses one, QuarantineAttachments sets it aside for good.

const (
	maxImageInput = 40 << 20 // larger pictures aren't decoded (memory on a Pi)
	maxPDFInput   = 20 << 20 // larger PDFs are described, not sent
)

// asideText replaces an attachment the model couldn't take.
const asideText = "[Attachment set aside: %s couldn't be read]"

// modelInput is what the model gets for one attachment.
type modelInput struct {
	kind string  // image | pdf | file | missing | aside
	data []byte  // the JPEG for an image, the PDF itself for a pdf
	why  unshown // why a picture isn't shown as one
	mime string  // the file's type, from its content
	size int64
}

// unshown is why a picture can't be shown to the model as one: what the
// model is told, and (by code) what the person who attached it is.
type unshown struct {
	code  string // large | unreadable | svg | convert | format | damaged; "" when it's shown
	model string
}

// user is the note for the person who attached the picture.
func (u unshown) user(mimeType string) string {
	switch u.code {
	case "":
		return ""
	case "large":
		return "This picture is too big for Alexa to look at. It’s saved in your files."
	case "unreadable":
		return "Alexa couldn’t open this picture just now."
	case "svg":
		return "Alexa can’t look at SVG drawings, only read their code."
	case "convert":
		return fmt.Sprintf("Alexa can’t look at %s photos here. A JPEG or PNG will work.", strings.ToUpper(strings.TrimPrefix(mimeType, "image/")))
	case "format":
		return "Alexa can’t look at pictures in this format. A JPEG or PNG will work."
	}
	return "This picture looks damaged, so Alexa can’t look at it."
}

// The cache keeps why a picture isn't shown as "<code>\n<model text>".
func (u unshown) encode() []byte { return []byte(u.code + "\n" + u.model) }

func decodeUnshown(b []byte) unshown {
	code, model, ok := strings.Cut(string(b), "\n")
	if !ok {
		return unshown{code: "damaged", model: code}
	}
	return unshown{code: code, model: model}
}

// attachmentContent is an attachment as message content parts.
func attachmentContent(ctx context.Context, env *userenv.Env, a Attachment) []map[string]any {
	in := loadInput(ctx, env, a)
	text := func(s string) map[string]any { return map[string]any{"type": "input_text", "text": s} }
	switch in.kind {
	case "aside":
		return []map[string]any{text(fmt.Sprintf(asideText, attachName(a)))}
	case "image":
		return []map[string]any{
			text(fmt.Sprintf("[Attached image %q, saved at %s]", a.Name, a.Path)),
			{"type": "input_image", "image_url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(in.data)},
		}
	case "pdf":
		return []map[string]any{
			text(fmt.Sprintf("[Attached PDF %q, saved at %s]", a.Name, a.Path)),
			{"type": "input_file", "filename": a.Name, "file_data": "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(in.data)},
		}
	}
	if in.why.code != "" {
		return []map[string]any{text(fmt.Sprintf("[Attached image %q (%s), saved at %s. It isn't shown to you: %s.]", a.Name, in.mime, a.Path, in.why.model))}
	}
	m := a.Mime
	if m == "" {
		m = in.mime
	}
	return []map[string]any{text(fmt.Sprintf("[Attached file %q (%s, %d bytes), saved in the workspace at %s]", a.Name, m, a.Size, a.Path))}
}

// attachName is the name an attachment goes by in the model's input, on
// one line: it came from the person's device or the client.
func attachName(a Attachment) string {
	name := a.Name
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(a.Path)
	}
	name = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)), " ")
	if r := []rune(name); len(r) > 120 {
		name = string(r[:119]) + "…"
	}
	return name
}

// loadInput works out what the model gets for an attachment, converting a
// picture (and remembering the result) the first time.
func loadInput(ctx context.Context, env *userenv.Env, a Attachment) modelInput {
	rel := env.Clean(a.Path)
	fi, err := env.Stat(rel)
	if err != nil || !fi.Mode().IsRegular() {
		return modelInput{kind: "missing"}
	}
	// What's kept for this version of the file: changed or replaced (a new
	// upload under the same name), it's a new file to the cache.
	variant := ""
	if env.Cache != "" {
		variant = cacheVariant(env, rel, fi)
		if _, err := os.Stat(variant + ".aside"); err == nil {
			return modelInput{kind: "aside"}
		}
	}
	in := modelInput{kind: "file", size: fi.Size()}
	head, err := readHead(env, rel)
	if err != nil {
		return modelInput{kind: "missing"}
	}
	in.mime = SniffMime(head, rel, a.Mime)
	if bytes.HasPrefix(head, []byte("%PDF-")) {
		in.mime = "application/pdf"
		if fi.Size() < maxPDFInput {
			if data, err := env.ReadFile(rel, maxPDFInput); err == nil {
				in.kind, in.data = "pdf", data
			}
		}
		return in
	}
	if !imageLike(head, rel, a.Mime) {
		return in
	}
	// A picture: use the JPEG made before, if the file hasn't changed.
	if variant != "" {
		if jpg, err := os.ReadFile(variant + ".jpg"); err == nil {
			in.kind, in.data = "image", jpg
			return in
		}
		if note, err := os.ReadFile(variant + ".txt"); err == nil {
			in.why = decodeUnshown(note)
			return in
		}
	}
	jpg, why, final := normalizeImage(ctx, env, rel, in.mime, fi.Size())
	if jpg != nil {
		in.kind, in.data = "image", jpg
	} else {
		in.why = why
	}
	if variant != "" && final {
		if jpg != nil {
			writeCache(variant+".jpg", jpg)
		} else {
			writeCache(variant+".txt", why.encode())
		}
	}
	return in
}

// cacheVariant names what the cache keeps for one version of a workspace
// file, without an extension.
func cacheVariant(env *userenv.Env, rel string, fi os.FileInfo) string {
	return filepath.Join(env.Cache, fmt.Sprintf("%s-%d-%d", cacheKey(rel), fi.Size(), fi.ModTime().UnixNano()))
}

func readHead(env *userenv.Env, rel string) ([]byte, error) {
	f, err := env.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return head[:n], nil
}

// normalizeImage makes the JPEG the model is shown, or says why it can't.
// final is false when trying again later might work (a read error, or a
// converter that was stopped or timed out), so the answer isn't kept.
func normalizeImage(ctx context.Context, env *userenv.Env, rel, ct string, size int64) (jpg []byte, why unshown, final bool) {
	if size > maxImageInput {
		return nil, unshown{"large", fmt.Sprintf("it's too large to show (%d MB)", size>>20)}, true
	}
	data, err := env.ReadFile(rel, maxImageInput)
	if err != nil {
		return nil, unshown{"unreadable", "it couldn't be read"}, false
	}
	if jpg, _, _, err = toJPEG(data); err == nil {
		return jpg, unshown{}, true
	}
	switch {
	case ct == "image/svg+xml":
		return nil, unshown{"svg", "it's an SVG drawing, which can't be shown as a picture; read_file shows its source"}, true
	case ct == "image/heic" || ct == "image/heif" || ct == "image/avif":
		out, cerr := convertImage(ctx, env, rel)
		if cerr == nil {
			if jpg, _, _, err := toJPEG(out); err == nil {
				return jpg, unshown{}, true
			}
		} else {
			slog.Debug("convert image", "path", rel, "err", cerr)
		}
		return nil, unshown{"convert", fmt.Sprintf("it's a %s picture, which can't be shown to you here", strings.ToUpper(strings.TrimPrefix(ct, "image/")))}, ctx.Err() == nil
	case strings.Contains(err.Error(), "too large to show"):
		return nil, unshown{"large", "it's " + err.Error()}, true
	case errors.Is(err, image.ErrFormat):
		return nil, unshown{"format", "it's in a format (" + ct + ") that can't be shown to you"}, true
	}
	return nil, unshown{"damaged", "the file looks damaged or incomplete"}, true
}

// convertImage turns a photo Go can't decode (HEIC, AVIF) into a JPEG with
// a tool that's installed: sips on a Mac, ffmpeg elsewhere. It runs as the
// user, as read_file's PDF tools do: the file came from them or the web.
func convertImage(ctx context.Context, env *userenv.Env, rel string) ([]byte, error) {
	full, err := env.Resolve(rel)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// sips only writes to a file. It's only on Macs, where nothing runs
	// under a user's own UID, so a folder of the server's own is fine.
	if sips := findTool("sips"); sips != "" && env.UID == 0 {
		dir, err := os.MkdirTemp("", "alexa-convert-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		out := filepath.Join(dir, "out.jpg")
		cmd := exec.CommandContext(ctx, sips, "-s", "format", "jpeg", full, "--out", out)
		asUser(cmd, env)
		if b, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("%w: %s", err, bytes.TrimSpace(b))
		}
		return os.ReadFile(out)
	}
	if ff := findTool("ffmpeg"); ff != "" {
		cmd := exec.CommandContext(ctx, ff, "-v", "error", "-i", full, "-frames:v", "1", "-f", "image2pipe", "-c:v", "mjpeg", "-")
		asUser(cmd, env)
		var out, stderr bytes.Buffer
		cmd.Stdout = &limitWriter{w: &out, n: maxImageInput}
		cmd.Stderr = &limitWriter{w: &stderr, n: 2000}
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("%w: %s", err, bytes.TrimSpace(stderr.Bytes()))
		}
		return out.Bytes(), nil
	}
	return nil, errors.New("no image converter is installed")
}

// SniffMime settles a file's type from its first bytes, falling back to
// its name and then to what the browser said. Neither the name nor the
// browser is taken at its word for a picture or a PDF.
func SniffMime(head []byte, name, declared string) string {
	sniffed := http.DetectContentType(head)
	base, _, _ := mime.ParseMediaType(sniffed)
	heif := heifBrand(head)
	switch {
	case strings.HasPrefix(base, "image/") || base == "application/pdf":
		return base
	case heif != "":
		return heif
	case bytes.HasPrefix(head, []byte("II*\x00")) || bytes.HasPrefix(head, []byte("MM\x00*")):
		return "image/tiff"
	case (base == "text/xml" || base == "text/plain") && bytes.Contains(bytes.ToLower(head), []byte("<svg")):
		return "image/svg+xml"
	}
	if m := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); m != "" && (!media(m) || strings.HasPrefix(m, "image/svg")) {
		return m
	}
	if d, _, _ := mime.ParseMediaType(declared); d != "" && d != "application/octet-stream" && !media(declared) {
		return declared
	}
	return sniffed
}

// media is a picture or a PDF: types only the content can vouch for.
func media(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, "application/pdf")
}

// heifBrand recognises HEIC, HEIF and AVIF files by their ftyp box.
func heifBrand(head []byte) string {
	if len(head) < 12 || string(head[4:8]) != "ftyp" {
		return ""
	}
	switch string(head[8:12]) {
	case "heic", "heix", "hevc", "hevx", "heim", "heis":
		return "image/heic"
	case "mif1", "msf1":
		return "image/heif"
	case "avif", "avis":
		return "image/avif"
	}
	return ""
}

// imageLike says whether a file is meant to be a picture, by its content,
// its name or its declared type; whether it really is one is up to the
// decoder.
func imageLike(head []byte, name, declared string) bool {
	if strings.HasPrefix(SniffMime(head, name, ""), "image/") || strings.HasPrefix(declared, "image/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".heic", ".heif", ".avif", ".svg":
		return true
	}
	return false
}

// cacheKey names a workspace path's files in the user's cache.
func cacheKey(rel string) string {
	sum := sha256.Sum256([]byte(rel))
	return hex.EncodeToString(sum[:12])
}

// writeCache writes a cache file whole, so a replay running at the same
// time never reads half of one.
func writeCache(path string, data []byte) {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		slog.Warn("attachment cache", "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

// PrepareAttachment runs when a file is uploaded: it settles the file's
// type from its content and makes the JPEG the model will be shown, so the
// first turn doesn't wait for it. The note says why a picture can't be
// shown, for the person who attached it; it's empty when all is well.
func PrepareAttachment(ctx context.Context, env *userenv.Env, a Attachment) (Attachment, string) {
	in := loadInput(ctx, env, a)
	if in.mime != "" {
		a.Mime = in.mime
	}
	if in.kind == "missing" {
		return a, ""
	}
	a.Size = in.size
	return a, in.why.user(in.mime)
}

// ForgetAttachment drops what the cache holds for a workspace path, when
// the file is deleted.
func ForgetAttachment(env *userenv.Env, p string) {
	if env.Cache == "" {
		return
	}
	// Every version's variants and set-aside marks: a new file under this
	// name starts afresh.
	key := cacheKey(env.Clean(p))
	matches, _ := filepath.Glob(filepath.Join(env.Cache, key+"-*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// QuarantineAttachments sets aside the pictures and documents of the
// messages the model hadn't answered yet (those after its last output, the
// ones that were waiting when a call failed), so a file it refuses can't
// make every later turn fail: from then on they're replayed as
// "[Attachment set aside: <name> couldn't be read]". It returns the names
// set aside; none means there was nothing to set aside, so retrying won't
// help.
//
// The loop calls it once when the model rejects its input
// (AttachmentRejected), then retries.
func (rt *Runtime) QuarantineAttachments(ctx context.Context, userID int64) ([]string, error) {
	return rt.setAside(ctx, userID, false)
}

// RepairAttachments is "Fix conversation": it sets aside what
// QuarantineAttachments would or, when nothing is waiting and older is set
// (the model named a picture or a file it refused), the pictures and
// documents of the newest message that has any. A request refused for
// anything else leaves older attachments alone: they were read before.
func (rt *Runtime) RepairAttachments(ctx context.Context, userID int64, older bool) ([]string, error) {
	return rt.setAside(ctx, userID, older)
}

func (rt *Runtime) setAside(ctx context.Context, userID int64, newest bool) ([]string, error) {
	env := rt.envs.For(userID)
	if env.Cache == "" {
		return nil, errors.New("no attachment cache")
	}
	if err := os.MkdirAll(env.Cache, 0o700); err != nil {
		return nil, err
	}
	_, rows, err := rt.st.Conversation(ctx, userID)
	if err != nil {
		return nil, err
	}
	var items []storedItem
	lastOut := -1
	for _, r := range rows {
		var si storedItem
		if err := json.Unmarshal(r.JSON, &si); err != nil || len(si.Item) == 0 {
			continue
		}
		if !isMessage(si.Kind) && llm.ParseItem(si.Item).Type != "function_call_output" {
			lastOut = len(items)
		}
		items = append(items, si)
	}
	media := func(si storedItem) []Attachment {
		var out []Attachment
		for _, a := range si.Attach {
			if k := loadInput(ctx, env, a).kind; k == "image" || k == "pdf" {
				out = append(out, a)
			}
		}
		return out
	}
	var pick []Attachment
	for _, si := range items[lastOut+1:] {
		if isMessage(si.Kind) {
			pick = append(pick, media(si)...)
		}
	}
	for i := len(items) - 1; newest && i >= 0 && len(pick) == 0; i-- {
		if isMessage(items[i].Kind) {
			pick = media(items[i])
		}
	}
	var names []string
	for _, a := range pick {
		rel := env.Clean(a.Path)
		fi, err := env.Stat(rel)
		if err != nil {
			continue // gone since: it's replayed as missing anyway
		}
		if err := os.WriteFile(cacheVariant(env, rel, fi)+".aside", []byte(attachName(a)), 0o600); err != nil {
			return names, err
		}
		names = append(names, attachName(a))
	}
	if len(names) > 0 {
		slog.Warn("attachments set aside", "user", userID, "names", names)
	}
	return names, nil
}

// AttachmentRejected says whether a model error looks like it refused an
// attachment (a picture or a file it couldn't read), rather than the
// request as a whole.
func AttachmentRejected(err error) bool {
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) || apiErr.Status < 400 || apiErr.Status >= 500 || isContextOverflow(err) {
		return false
	}
	switch apiErr.Status {
	case 401, 403, 404, 408, 409, 429:
		return false
	}
	if apiErr.Param != "" && !strings.HasPrefix(apiErr.Param, "input") {
		// It names what it refused, and that isn't the conversation: a
		// tool's schema ("Invalid schema for function 'read_file'").
		return false
	}
	s := strings.ToLower(apiErr.Message + " " + apiErr.Param + " " + apiErr.Code)
	for _, w := range []string{"image", "file", "pdf", "attachment", "media", "base64", "mime", "decode"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}
