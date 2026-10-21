package vault

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alexagent/internal/store"
)

func TestSite(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.amazon.co.uk/ap/signin?x=1": "amazon.co.uk",
		"accounts.google.com":                    "google.com",
		"Login.Example.COM.":                     "example.com",
		"example.com/login":                      "example.com",
		"example.com:8443":                       "example.com",
		"https://alice.github.io/app":            "alice.github.io",
		"http://127.0.0.1:8080/login":            "127.0.0.1",
		"http://[::1]:3000/":                     "::1",
		"localhost":                              "localhost",
		"https://xn--bcher-kva.de/konto":         "xn--bcher-kva.de",
	} {
		if got, err := Site(in); err != nil || got != want {
			t.Errorf("Site(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "amazon", "co.uk", "https://", "bücher.de"} {
		if got, err := Site(in); err == nil {
			t.Errorf("Site(%q) = %q, want an error", in, got)
		}
	}
}

func TestAllowed(t *testing.T) {
	for _, c := range []struct {
		page, site string
		ok         bool
	}{
		{"https://www.amazon.com/ap/signin", "amazon.com", true},
		{"https://amazon.com.evil.net/signin", "amazon.com", false},
		{"https://evil-amazon.com/", "amazon.com", false},
		{"http://www.amazon.com/ap/signin", "amazon.com", false}, // no HTTPS
		{"http://127.0.0.1:8080/login", "127.0.0.1", true},       // this machine
		{"http://localhost:3000/", "localhost", true},
		{"about:blank", "amazon.com", false},
		{"data:text/html,<input type=password>", "amazon.com", false},
	} {
		if err := Allowed(c.page, c.site); (err == nil) != c.ok {
			t.Errorf("Allowed(%q, %q) = %v, want ok=%v", c.page, c.site, err, c.ok)
		}
	}
}

func openTest(t *testing.T) (*Vault, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	key := filepath.Join(dir, "vault.key")
	v, err := Open(key, st)
	if err != nil {
		t.Fatal(err)
	}
	return v, st, key
}

func TestVault(t *testing.T) {
	v, st, key := openTest(t)
	ctx := context.Background()
	alice, _ := st.CreateUser(ctx, "alice", "x", "Alice", true)
	bob, _ := st.CreateUser(ctx, "bob", "x", "Bob", false)

	if fi, err := os.Stat(key); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", fi, err)
	}
	l, err := v.Save(ctx, alice.ID, "https://www.amazon.com/ap/signin", "alice@example.com", "hunter2-amazon")
	if err != nil {
		t.Fatal(err)
	}
	if l.Site != "amazon.com" {
		t.Fatalf("saved under %q", l.Site)
	}
	if bytes.Contains(l.Secret, []byte("hunter2")) {
		t.Fatal("the password is stored in the clear")
	}
	// Found on any page of the site, and only for its owner.
	got, err := v.Find(ctx, alice.ID, "amazon.com", "")
	if err != nil || len(got) != 1 || got[0].Password != "hunter2-amazon" || got[0].Username != "alice@example.com" {
		t.Fatalf("find: %+v %v", got, err)
	}
	if got, _ := v.Find(ctx, bob.ID, "amazon.com", ""); len(got) != 0 {
		t.Fatalf("bob found alice's login: %+v", got)
	}

	// Saving the same account again replaces its password.
	if _, err := v.Save(ctx, alice.ID, "amazon.com", "alice@example.com", "new-pass"); err != nil {
		t.Fatal(err)
	}
	if list, _ := v.List(ctx, alice.ID); len(list) != 1 {
		t.Fatalf("logins = %d, want 1", len(list))
	}
	if got, _ := v.Find(ctx, alice.ID, "amazon.com", ""); got[0].Password != "new-pass" {
		t.Fatalf("password = %q", got[0].Password)
	}

	// A ciphertext moved to another user's login doesn't decrypt.
	stolen := &store.Login{UserID: bob.ID, Site: "amazon.com", Username: "alice@example.com", Secret: l.Secret}
	if err := st.SaveLogin(ctx, stolen); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Find(ctx, bob.ID, "amazon.com", ""); err == nil {
		t.Fatal("a password moved to another user decrypted")
	}

	// Renaming the account keeps the password.
	upd, err := v.Update(ctx, alice.ID, l.ID, "alice@work.example", "")
	if err != nil || upd.Username != "alice@work.example" {
		t.Fatalf("update: %+v %v", upd, err)
	}
	if got, _ := v.Find(ctx, alice.ID, "amazon.com", "alice@work.example"); len(got) != 1 || got[0].Password != "new-pass" {
		t.Fatalf("after rename: %+v", got)
	}

	// The same key opens the vault again; another key doesn't.
	again, err := Open(key, st)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := again.Find(ctx, alice.ID, "amazon.com", ""); err != nil || len(got) != 1 {
		t.Fatalf("reopened vault: %+v %v", got, err)
	}
	other, err := Open(filepath.Join(t.TempDir(), "other.key"), st)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Find(ctx, alice.ID, "amazon.com", ""); err == nil || !strings.Contains(err.Error(), "decrypted") {
		t.Fatalf("a different key: %v", err)
	}
	if err := os.WriteFile(key, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(key, st); err == nil {
		t.Fatal("opened a vault with a broken key file")
	}
}

func TestHold(t *testing.T) {
	v, st, _ := openTest(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "carol", "x", "Carol", true)
	if _, err := v.Save(ctx, u.ID, "github.com", "carol", "saved-pass"); err != nil {
		t.Fatal(err)
	}
	if err := v.Hold(u.ID, "https://github.com/login", "carol-work", "held-pass"); err != nil {
		t.Fatal(err)
	}
	// The login just entered comes first; the saved one is still there.
	got, err := v.Find(ctx, u.ID, "github.com", "")
	if err != nil || len(got) != 2 || got[0].Username != "carol-work" || got[0].ID != 0 || got[1].Password != "saved-pass" {
		t.Fatalf("find: %+v %v", got, err)
	}
	if names, _ := v.Accounts(ctx, u.ID, "github.com"); strings.Join(names, ",") != "carol-work,carol" {
		t.Fatalf("accounts = %v", names)
	}
	if list, _ := v.List(ctx, u.ID); len(list) != 1 {
		t.Fatalf("a held login was saved: %d logins", len(list))
	}
	// Held logins are forgotten after a while.
	v.mu.Lock()
	v.held[u.ID][0].until = time.Now().Add(-time.Second)
	v.mu.Unlock()
	if got, _ := v.Find(ctx, u.ID, "github.com", ""); len(got) != 1 || got[0].Username != "carol" {
		t.Fatalf("after expiry: %+v", got)
	}
}
