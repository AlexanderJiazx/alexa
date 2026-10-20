package agent

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alexagent/internal/userenv"
)

func viewCtx(t *testing.T) *toolCtx {
	dir := t.TempDir()
	return &toolCtx{ctx: context.Background(), run: &run{env: &userenv.Env{Workspace: dir, Home: dir}}}
}

func TestReadFileShowsImages(t *testing.T) {
	tc := viewCtx(t)
	img := image.NewRGBA(image.Rect(0, 0, 3000, 1000))
	img.Set(10, 10, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tc.run.env.Workspace, "wide.png"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := readFile(tc, map[string]any{"path": "wide.png"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Showing image wide.png (3000×1000)." || len(res.Image) == 0 {
		t.Fatalf("got %q with %d image bytes", res.Text, len(res.Image))
	}
	shown, err := jpeg.Decode(bytes.NewReader(res.Image))
	if err != nil {
		t.Fatal(err)
	}
	if b := shown.Bounds(); b.Dx() != maxViewSide || b.Dy() != 533 {
		t.Errorf("shown at %v, want %dx533", b, maxViewSide)
	}
}

// A minimal PDF; poppler rebuilds the missing cross-reference table.
const helloPDF = `%PDF-1.4
1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj
2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj
3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 300 144] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >> endobj
4 0 obj << /Length 41 >> stream
BT /F1 18 Tf 20 60 Td (Hello PDF) Tj ET
endstream endobj
5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj
trailer << /Root 1 0 R >>
%%EOF
`

func TestReadFilePDF(t *testing.T) {
	if findTool("pdftotext") == "" || findTool("pdftoppm") == "" {
		t.Skip("poppler-utils not installed")
	}
	tc := viewCtx(t)
	if err := os.WriteFile(filepath.Join(tc.run.env.Workspace, "hello.pdf"), []byte(helloPDF), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := readFile(tc, map[string]any{"path": "hello.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Hello PDF") || len(res.Image) != 0 {
		t.Errorf("text read: %q (image %d bytes)", res.Text, len(res.Image))
	}
	res, err = readFile(tc, map[string]any{"path": "hello.pdf", "page": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "is shown below") || !strings.Contains(res.Text, "Hello PDF") || len(res.Image) == 0 {
		t.Errorf("page view: %q (image %d bytes)", res.Text, len(res.Image))
	}
}
