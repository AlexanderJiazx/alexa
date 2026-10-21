package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
)

// Files pages download land in the user's downloads folder. Chrome saves
// each under its own id; once it's done it gets its name, with a number
// added when the folder already has a file by that name, so one download
// never replaces another. The next page state the agent gets says what
// arrived, so it doesn't take a download for a click that did nothing.

type download struct {
	guid  string
	name  string // the file's name in the downloads folder
	state cdpbrowser.DownloadProgressState
	bytes float64
}

type downloads struct {
	dir  string
	mu   sync.Mutex
	list []*download
}

func (ds *downloads) event(ev any) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	switch e := ev.(type) {
	case *cdpbrowser.EventDownloadWillBegin:
		name := filepath.Base(e.SuggestedFilename)
		if name == "." || name == "/" || name == "" {
			name = "download"
		}
		ds.list = append(ds.list, &download{guid: e.GUID, name: name, state: cdpbrowser.DownloadProgressStateInProgress})
	case *cdpbrowser.EventDownloadProgress:
		for _, d := range ds.list {
			if d.guid != e.GUID {
				continue
			}
			d.state, d.bytes = e.State, e.ReceivedBytes
			if e.State == cdpbrowser.DownloadProgressStateCompleted {
				from := e.FilePath
				if from == "" {
					from = filepath.Join(ds.dir, e.GUID)
				}
				d.name = place(from, ds.dir, d.name)
			}
		}
	}
}

// take returns the downloads not reported yet, waiting up to wait for those
// in progress to finish. One still in progress is reported again once it's
// done.
func (ds *downloads) take(wait time.Duration) []download {
	deadline := time.Now().Add(wait)
	for {
		ds.mu.Lock()
		pending := false
		for _, d := range ds.list {
			pending = pending || d.state == cdpbrowser.DownloadProgressStateInProgress
		}
		if !pending || time.Now().After(deadline) {
			var out []download
			var left []*download
			for _, d := range ds.list {
				out = append(out, *d)
				if d.state == cdpbrowser.DownloadProgressStateInProgress {
					left = append(left, d)
				}
			}
			ds.list = left
			ds.mu.Unlock()
			return out
		}
		ds.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
}

// place gives the file at from its name in dir, adding a number if dir has
// a file by that name, and returns the name it got.
func place(from, dir, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s-%d%s", base, i, ext)
		}
		to := filepath.Join(dir, n)
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		// A link fails if the name was taken meanwhile, so nothing is
		// replaced. Where there are no hard links (some USB drives), rename.
		err := os.Link(from, to)
		if err == nil {
			_ = os.Remove(from)
			return n
		}
		if os.IsExist(err) {
			continue
		}
		if os.Rename(from, to) == nil {
			return n
		}
		break
	}
	return filepath.Base(from)
}

// downloadNote tells the agent about downloads in the page state.
func downloadNote(ds []download) string {
	var b strings.Builder
	for _, d := range ds {
		switch d.state {
		case cdpbrowser.DownloadProgressStateCompleted:
			fmt.Fprintf(&b, "Note: downloaded %s to downloads/ (%s).\n", d.name, size(d.bytes))
		case cdpbrowser.DownloadProgressStateCanceled:
			fmt.Fprintf(&b, "Note: the download of %s failed.\n", d.name)
		default:
			fmt.Fprintf(&b, "Note: still downloading %s (%s so far); it lands in downloads/.\n", d.name, size(d.bytes))
		}
	}
	return b.String()
}

func size(n float64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", n/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", n/(1<<10))
	}
	return fmt.Sprintf("%.0f bytes", n)
}
