package browser

import (
	"errors"
	"os"
	"testing"
	"time"
)

// Removing a user closes their browser for good: one starting as they're
// removed is closed before Remove returns, and none starts for them again
// (BH-5). Other users are untouched.
func TestManagerRemove(t *testing.T) {
	if os.Getenv("SKIP_BROWSER_TESTS") != "" {
		t.Skip("browser tests disabled")
	}
	dir := chromeDir(t)
	m := NewManager(Options{Headless: true, Width: 800, Height: 600, IdleAfter: time.Hour, ChromePath: os.Getenv("CHROME_PATH")},
		func(id int64) UserEnv {
			d := dir + "/" + string(rune('0'+id))
			return UserEnv{UserID: id, ProfileDir: d + "/profile", DownloadDir: d + "/dl", HomeDir: d}
		}, nil)
	t.Cleanup(m.CloseAll)

	// Removed while it launches.
	type got struct {
		b   *Browser
		err error
	}
	res := make(chan got, 1)
	go func() { b, err := m.Get(1); res <- got{b, err} }()
	for i := 0; i < 200; i++ {
		m.mu.Lock()
		_, starting := m.pending[1]
		m.mu.Unlock()
		if starting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.Remove(1)
	r := <-res
	if !errors.Is(r.err, ErrRemoved) || r.b != nil {
		t.Errorf("a launch for a user removed meanwhile = %v, %v; want ErrRemoved", r.b, r.err)
	}
	m.mu.Lock()
	n := len(m.browsers)
	m.mu.Unlock()
	if n != 0 {
		t.Errorf("%d browsers kept after the removal", n)
	}
	if _, err := m.Get(1); !errors.Is(err, ErrRemoved) {
		t.Errorf("Get after Remove = %v, want ErrRemoved", err)
	}

	// Removed with a browser open.
	b, err := m.Get(2)
	if err != nil {
		t.Fatal(err)
	}
	m.Remove(2)
	if b.alive() {
		t.Error("the removed user's browser is still open")
	}
	if _, err := m.Get(2); !errors.Is(err, ErrRemoved) {
		t.Errorf("Get after Remove = %v, want ErrRemoved", err)
	}
	if _, err := m.Get(3); err != nil {
		t.Errorf("another user's browser: %v", err)
	}
}
