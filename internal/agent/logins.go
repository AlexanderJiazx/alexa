package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"alexagent/internal/browser"
	"alexagent/internal/store"
	"alexagent/internal/vault"
)

// Signing in to sites with the user's logins (see internal/vault). Alexa
// picks the fields to fill; the username and password never reach the
// model, and a login is only typed into the site it belongs to.

// ErrNotWaiting is returned for a login Alexa didn't ask for.
var ErrNotWaiting = errors.New("Alexa isn't waiting for a login")

func browserLoginTool() *tool {
	return browserTool("browser_login", "Sign in with the user's saved login for the site open in the browser. The system types the username and password into the fields you give; you never see the password. Give the index of the username or email field, of the password field, or both (when a site asks for them on separate pages, call this on each page). "+
		"If there's no saved login for the site, it says so: then call request_login.",
		map[string]any{
			"username_index": p("integer", "Index of the username or email field"),
			"password_index": p("integer", "Index of the password field"),
			"username":       p("string", "Which saved account to use, when there are several for the site"),
			"submit":         p("boolean", "Press Enter after filling in"),
		}, nil, browserLogin)
}

func requestLoginTool() *tool {
	return &tool{
		Name: "request_login",
		Description: "Ask the user to sign in to a site through a secure form on their screen: they type their username and password there, and can save them so you can sign in by yourself next time (for example when the session expires). You never see the password. " +
			"When they've entered it, call browser_login to fill in the sign-in form. Use this when browser_login says there's no saved login for the site, or the saved one was rejected.",
		Params: map[string]any{
			"reason": p("string", "Short note shown above the form, e.g. \"Your Amazon session expired\""),
			"site":   p("string", "The site to sign in to, when it isn't the one open in the browser (e.g. the sign-in form is in a frame from another site)"),
		},
		Required: []string{"reason"},
		Run:      requestLogin,
	}
}

func requestLogin(tc *toolCtx, a map[string]any) (*toolResult, error) {
	addr := strings.TrimSpace(str(a, "site"))
	if addr == "" {
		tab := tc.rt.peekTab(tc.run.user.ID)
		if tab == nil {
			return nil, errors.New("the browser isn't open: open the site's sign-in page first")
		}
		addr, _ = tab.URL()
		if !strings.HasPrefix(addr, "http") {
			return nil, errors.New("open the site's sign-in page in the browser first")
		}
	}
	site, err := vault.Site(addr)
	if err != nil {
		return nil, err
	}
	nd := &Needs{Kind: "login", Message: trim(cleanText(str(a, "reason")), 200), Site: site}
	if nd.Message == "" {
		nd.Message = "Sign in to " + site
	}
	// Suggest the account last used there, e.g. when its password was
	// rejected.
	if names, err := tc.rt.vault.Accounts(tc.ctx, tc.run.user.ID, site); err == nil && len(names) > 0 {
		nd.Username = names[0]
	}
	tc.run.replied()
	return &toolResult{Suspend: nd}, nil
}

func browserLogin(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
	userIdx, passIdx := int(num(a, "username_index", 0)), int(num(a, "password_index", 0))
	if userIdx <= 0 && passIdx <= 0 {
		return "", errors.New("give username_index, password_index or both")
	}
	field := passIdx
	if field <= 0 {
		field = userIdx
	}
	page, err := tab.FieldURL(field)
	if err != nil {
		return "", err
	}
	site, err := vault.Site(page)
	if err != nil {
		return "", errors.New("that field isn't on a website")
	}
	uid := tc.run.user.ID
	name := strings.TrimSpace(str(a, "username"))
	logins, err := tc.rt.vault.Find(tc.ctx, uid, site, name)
	if err != nil {
		return "", err
	}
	if len(logins) == 0 {
		if names, _ := tc.rt.vault.Accounts(tc.ctx, uid, site); name != "" && len(names) > 0 {
			return "", fmt.Errorf("there's no saved login for %s with the username %q; the saved ones are %s", site, name, strings.Join(names, ", "))
		}
		return "", fmt.Errorf("there's no saved login for %s. Call request_login so the user can enter it", site)
	}
	l := logins[0]
	allow := func(pageURL string) error { return vault.Allowed(pageURL, l.Site) }
	var filled []string
	if userIdx > 0 {
		if err := tab.Fill(userIdx, l.Username, false, allow); err != nil {
			return "", err
		}
		filled = append(filled, "username")
	}
	if passIdx > 0 {
		if err := tab.Fill(passIdx, l.Password, true, allow); err != nil {
			return "", err
		}
		filled = append(filled, "password")
	}
	tc.rt.vault.Used(tc.ctx, uid, l)
	msg := fmt.Sprintf("Filled in the %s of the login for %s (username %s)", strings.Join(filled, " and "), site, l.Username)
	if boolean(a, "submit", false) {
		if err := tab.Keys("Enter"); err != nil {
			return msg, err
		}
		msg += " and pressed Enter"
	}
	msg += "."
	if len(logins) > 1 && name == "" {
		var others []string
		for _, o := range logins[1:] {
			others = append(others, o.Username)
		}
		msg += fmt.Sprintf(" Other accounts saved for %s: %s (pass username to use one).", site, strings.Join(others, ", "))
	}
	return msg, nil
}

// SignIn answers Alexa's request to sign in to a site: the login the user
// entered in the secure form is saved, or with save unset held for this
// sign-in only, and Alexa goes on to fill it in. The password never enters
// the conversation.
func (rt *Runtime) SignIn(ctx context.Context, u *store.User, username, password string, save bool) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return errors.New("enter your username and password")
	}
	l := rt.userLock(u.ID)
	l.Lock()
	defer l.Unlock()
	nd := rt.pendingNeeds(ctx, u.ID)
	if nd == nil || nd.Kind != "login" {
		return ErrNotWaiting
	}
	kept := "saved it for next time"
	if save {
		if _, err := rt.vault.Save(ctx, u.ID, nd.Site, username, password); err != nil {
			return err
		}
	} else {
		if err := rt.vault.Hold(u.ID, nd.Site, username, password); err != nil {
			return err
		}
		kept = "chose not to save it, so it's kept only for the next few minutes"
	}
	_ = rt.addEntry(ctx, &store.Entry{UserID: u.ID, Kind: "step", Text: "You entered your login for " + nd.Site, Status: "done"})
	answer := fmt.Sprintf("The user entered their login for %s in the secure form (username %s) and %s. Call browser_login to fill in the sign-in form.", nd.Site, username, kept)
	if err := rt.resolveNeeds(ctx, u.ID, nd, stamp(userLoc(u), "", answer), nil); err != nil {
		return err
	}
	env, err := rt.envs.Ensure(u.ID)
	if err != nil {
		return err
	}
	rt.launch(u, env, false)
	return nil
}
