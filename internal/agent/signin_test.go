package agent

import (
	"testing"
)

// Handing the browser back answers the question Alexa is waiting on only
// if the run that reads the answer can start: when the user's folders
// can't be had, the question stays open to answer again (BH-10).
func TestHandBackKeepsTheQuestionWhenFoldersFail(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(fc("ask", map[string]any{"question": "Which dates?", "options": []string{"May", "June"}})))
	e.send("Book me a hotel in Lisbon")
	if v := e.settle(); v.Status != "needs" {
		t.Fatalf("status = %q, want needs", v.Status)
	}
	restore := breakFolders(t, e)
	if err := e.rt.HandBack(e.ctx, e.user, ""); err == nil {
		t.Fatal("HandBack went ahead without the user's folders")
	}
	if v := e.rt.View(e.ctx, e.user.ID); v.Status != "needs" || e.rt.pendingNeeds(e.ctx, e.user.ID) == nil {
		t.Fatalf("after a failed hand-back the status is %q, want still needs", v.Status)
	}
	restore()
	e.model.script(does(fc("say", map[string]any{"text": "Back to the hotel.", "final": true})), does())
	if err := e.rt.HandBack(e.ctx, e.user, ""); err != nil {
		t.Fatal(err)
	}
	if v := e.settle(); v.Status == "needs" {
		t.Errorf("the question is still open after handing back")
	}
}

// The same for a login entered in the secure form.
func TestSignInKeepsTheRequestWhenFoldersFail(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(fc("request_login", map[string]any{"reason": "Your session expired", "site": "https://shop.example"})))
	e.send("Order my usual coffee")
	if v := e.settle(); v.Status != "needs" {
		t.Fatalf("status = %q, want needs", v.Status)
	}
	restore := breakFolders(t, e)
	if err := e.rt.SignIn(e.ctx, e.user, "ana", "secret-pass", false); err == nil {
		t.Fatal("SignIn went ahead without the user's folders")
	}
	if nd := e.rt.pendingNeeds(e.ctx, e.user.ID); nd == nil || nd.Kind != "login" {
		t.Fatalf("after a failed sign-in the request is %+v, want the login still asked for", nd)
	}
	restore()
	e.model.script(does(fc("say", map[string]any{"text": "Signed in.", "final": true})), does())
	if err := e.rt.SignIn(e.ctx, e.user, "ana", "secret-pass", false); err != nil {
		t.Fatal(err)
	}
	if v := e.settle(); v.Status == "needs" {
		t.Errorf("the login is still asked for after signing in")
	}
}
