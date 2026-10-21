// Package vault keeps the logins people save for Alexa to sign in with.
// Passwords are encrypted with a key kept outside the database, are never
// shown to the model or sent back to the app, and are only typed into pages
// on the site they belong to.
package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"

	"alexagent/internal/store"
)

// Vault encrypts, stores and finds users' logins.
type Vault struct {
	st   *store.Store
	aead cipher.AEAD

	mu   sync.Mutex
	held map[int64][]Login // entered for one sign-in and not saved, by user
}

// Login is a login with its password, ready to be filled in.
type Login struct {
	ID       int64 // 0 for one held only for the current sign-in
	Site     string
	Username string
	Password string
	until    time.Time // when a held login is forgotten
}

// holdFor is how long a login the user chose not to save is kept: long
// enough to sign in with it, over several pages if need be.
const holdFor = 15 * time.Minute

// Open loads the key from keyFile, creating a new random key there if the
// file doesn't exist yet.
func Open(keyFile string, st *store.Store) (*Vault, error) {
	key, err := loadKey(keyFile)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{st: st, aead: aead, held: map[int64][]Login{}}, nil
}

// loadKey reads a 32-byte key, base64-encoded, from path, or makes one.
func loadKey(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			return loadKey(path) // made by someone else meanwhile
		}
		if err != nil {
			return nil, fmt.Errorf("create vault key: %w", err)
		}
		_, err = f.WriteString(base64.StdEncoding.EncodeToString(key) + "\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(path)
			return nil, fmt.Errorf("write vault key: %w", err)
		}
		return key, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read vault key: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("%s doesn't hold a vault key (32 bytes, base64-encoded)", path)
	}
	return key, nil
}

// additional ties a password to its user, site and username, so it can't
// be decrypted as part of another login.
func additional(userID int64, site, username string) []byte {
	return fmt.Appendf(nil, "alexa login\x00%d\x00%s\x00%s", userID, site, username)
}

func (v *Vault) seal(userID int64, site, username, password string) []byte {
	nonce := make([]byte, v.aead.NonceSize(), v.aead.NonceSize()+len(password)+v.aead.Overhead())
	_, _ = rand.Read(nonce)
	return v.aead.Seal(nonce, nonce, []byte(password), additional(userID, site, username))
}

func (v *Vault) open(l *store.Login) (string, error) {
	n := v.aead.NonceSize()
	if len(l.Secret) >= n {
		if pw, err := v.aead.Open(nil, l.Secret[:n], l.Secret[n:], additional(l.UserID, l.Site, l.Username)); err == nil {
			return string(pw), nil
		}
	}
	return "", fmt.Errorf("the saved password for %s can't be decrypted (was the vault key replaced?); enter it again", l.Site)
}

// Save saves a login for a site (given as a URL, host or domain), replacing
// the password of a saved login with the same username.
func (v *Vault) Save(ctx context.Context, userID int64, site, username, password string) (*store.Login, error) {
	site, err := Site(site)
	if err != nil {
		return nil, err
	}
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, errors.New("give a username and a password")
	}
	l := &store.Login{UserID: userID, Site: site, Username: username, Secret: v.seal(userID, site, username, password)}
	if err := v.st.SaveLogin(ctx, l); err != nil {
		return nil, err
	}
	return l, nil
}

// Update changes a saved login's username, its password, or both; an empty
// value leaves that part as it is.
func (v *Vault) Update(ctx context.Context, userID, id int64, username, password string) (*store.Login, error) {
	l, err := v.st.GetLogin(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if password == "" {
		if password, err = v.open(l); err != nil {
			return nil, err
		}
	}
	if u := strings.TrimSpace(username); u != "" {
		l.Username = u
	}
	l.Secret = v.seal(userID, l.Site, l.Username, password)
	if err := v.st.UpdateLogin(ctx, l); err != nil {
		return nil, err
	}
	return v.st.GetLogin(ctx, userID, id)
}

// List returns the user's saved logins (without their passwords).
func (v *Vault) List(ctx context.Context, userID int64) ([]*store.Login, error) {
	return v.st.Logins(ctx, userID)
}

func (v *Vault) Delete(ctx context.Context, userID, id int64) (bool, error) {
	return v.st.DeleteLogin(ctx, userID, id)
}

// Hold keeps a login the user entered for one sign-in, without saving it.
func (v *Vault) Hold(userID int64, site, username, password string) error {
	site, err := Site(site)
	if err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return errors.New("give a username and a password")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	keep := []Login{{Site: site, Username: username, Password: password, until: time.Now().Add(holdFor)}}
	for _, l := range v.live(userID) {
		if l.Site != site || !strings.EqualFold(l.Username, username) {
			keep = append(keep, l)
		}
	}
	v.held[userID] = keep
	return nil
}

// live returns the user's held logins that haven't expired yet. v.mu is
// held.
func (v *Vault) live(userID int64) []Login {
	now := time.Now()
	var keep []Login
	for _, l := range v.held[userID] {
		if now.Before(l.until) {
			keep = append(keep, l)
		}
	}
	if len(keep) == 0 {
		delete(v.held, userID)
	} else {
		v.held[userID] = keep
	}
	return keep
}

// Find returns the user's logins for a site in the order to try them: one
// just entered for this sign-in first, then saved ones, the most recently
// used first. With username set, only that account's.
func (v *Vault) Find(ctx context.Context, userID int64, site, username string) ([]Login, error) {
	var out []Login
	v.mu.Lock()
	for _, l := range v.live(userID) {
		if l.Site == site && (username == "" || strings.EqualFold(l.Username, username)) {
			out = append(out, l)
		}
	}
	v.mu.Unlock()
	saved, err := v.st.SiteLogins(ctx, userID, site)
	if err != nil {
		return nil, err
	}
	for _, s := range saved {
		if (username != "" && !strings.EqualFold(s.Username, username)) || has(out, s.Username) {
			continue
		}
		pw, err := v.open(s)
		if err != nil {
			return nil, err
		}
		out = append(out, Login{ID: s.ID, Site: s.Site, Username: s.Username, Password: pw})
	}
	return out, nil
}

// Accounts lists the usernames the user has for a site, in the order Find
// tries them, without decrypting anything.
func (v *Vault) Accounts(ctx context.Context, userID int64, site string) ([]string, error) {
	var out []string
	v.mu.Lock()
	for _, l := range v.live(userID) {
		if l.Site == site {
			out = append(out, l.Username)
		}
	}
	v.mu.Unlock()
	saved, err := v.st.SiteLogins(ctx, userID, site)
	if err != nil {
		return nil, err
	}
	for _, s := range saved {
		if !hasName(out, s.Username) {
			out = append(out, s.Username)
		}
	}
	return out, nil
}

// Used records that a saved login was used to sign in.
func (v *Vault) Used(ctx context.Context, userID int64, l Login) {
	if l.ID > 0 {
		_ = v.st.LoginUsed(ctx, userID, l.ID)
	}
}

func has(list []Login, username string) bool {
	for _, l := range list {
		if strings.EqualFold(l.Username, username) {
			return true
		}
	}
	return false
}

func hasName(list []string, username string) bool {
	for _, u := range list {
		if strings.EqualFold(u, username) {
			return true
		}
	}
	return false
}

// Site returns the site a web address belongs to, which logins are saved
// under and matched on: the registrable domain of its host, so a login for
// example.com also works on www.example.com and login.example.com. An IP
// address or localhost is its own site. It takes a URL, a host or a domain.
func Site(addr string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(addr))
	if strings.Contains(host, "://") {
		u, err := url.Parse(host)
		if err != nil {
			return "", badAddress(addr)
		}
		host = u.Hostname()
	} else {
		if i := strings.IndexAny(host, "/?#"); i >= 0 {
			host = host[:i]
		}
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	if net.ParseIP(host) != nil || host == "localhost" {
		return host, nil
	}
	// Browsers give internationalised names in their ASCII (punycode) form,
	// which is what logins are matched on; the app converts what people type.
	if !strings.Contains(host, ".") || strings.ContainsFunc(host, func(r rune) bool { return r > 0x7f }) {
		return "", badAddress(addr)
	}
	site, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return "", badAddress(addr)
	}
	return site, nil
}

func badAddress(addr string) error {
	return fmt.Errorf("%q isn't a website address", strings.TrimSpace(addr))
}

// Allowed reports whether a login for site may be typed into the page at
// pageURL: the page must be on that site and, unless it's on this machine,
// use HTTPS.
func Allowed(pageURL, site string) error {
	u, err := url.Parse(pageURL)
	if err != nil || u.Hostname() == "" {
		return errors.New("the sign-in form isn't on a website")
	}
	if got, err := Site(u.Hostname()); err != nil || got != site {
		return fmt.Errorf("the sign-in form is on %s, and a login for %s is only typed into %s", u.Hostname(), site, site)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback(u.Hostname())) {
		return fmt.Errorf("%s isn't using a secure connection (HTTPS), so the login isn't typed in", u.Hostname())
	}
	return nil
}

func loopback(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
