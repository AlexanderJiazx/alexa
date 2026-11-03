package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// An email provider that never answers doesn't hold the notification, or
// whatever sent it, forever (BH-13).
func TestSendEmailTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)
	if mailClient.Timeout != mailTimeout || mailTimeout <= 0 {
		t.Fatalf("the email client has no timeout: %s", mailClient.Timeout)
	}
	oldURL, oldClient := mailURL, mailClient
	mailURL, mailClient = srv.URL, &http.Client{Timeout: 200 * time.Millisecond}
	defer func() { mailURL, mailClient = oldURL, oldClient }()

	n := &Notifier{resendKey: "k", mailFrom: "a@example.com"}
	t0 := time.Now()
	err := n.sendEmail(context.Background(), "b@example.com", "s", "b", "")
	if err == nil {
		t.Fatal("a provider that never answered was taken as a sent email")
	}
	if d := time.Since(t0); d > 3*time.Second {
		t.Errorf("sendEmail took %s", d)
	}
}
