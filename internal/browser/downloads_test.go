package browser

import (
	"os"
	"path/filepath"
	"testing"

	cdpbrowser "github.com/chromedp/cdproto/browser"
)

// A download that lands says so once, under its own name, whether or not
// the agent reads a page state after it.
func TestDownloadLandedIsAnnounced(t *testing.T) {
	dir := t.TempDir()
	landed := 0
	ds := &downloads{dir: dir, done: func() { landed++ }}
	ds.event(&cdpbrowser.EventDownloadWillBegin{GUID: "g1", SuggestedFilename: "receipt.pdf"})
	ds.event(&cdpbrowser.EventDownloadProgress{GUID: "g1", State: cdpbrowser.DownloadProgressStateInProgress, ReceivedBytes: 10})
	if landed != 0 {
		t.Fatalf("announced while still downloading")
	}
	if err := os.WriteFile(filepath.Join(dir, "g1"), []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	ds.event(&cdpbrowser.EventDownloadProgress{GUID: "g1", State: cdpbrowser.DownloadProgressStateCompleted, ReceivedBytes: 3})
	if landed != 1 {
		t.Fatalf("landed = %d, want 1", landed)
	}
	if _, err := os.Stat(filepath.Join(dir, "receipt.pdf")); err != nil {
		t.Errorf("the file didn't get its name: %v", err)
	}
	// A download that fails isn't announced.
	ds.event(&cdpbrowser.EventDownloadWillBegin{GUID: "g2", SuggestedFilename: "x.zip"})
	ds.event(&cdpbrowser.EventDownloadProgress{GUID: "g2", State: cdpbrowser.DownloadProgressStateCanceled})
	if landed != 1 {
		t.Errorf("a cancelled download was announced")
	}
}
