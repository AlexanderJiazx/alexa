// Package notify delivers notifications in-app, via Web Push and by email.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"alexagent/internal/hub"
	"alexagent/internal/store"
)

type Notifier struct {
	st        *store.Store
	hub       *hub.Hub
	vapidPub  string
	vapidPriv string
	subject   string
	resendKey string
	mailFrom  string
	publicURL string
}

func New(ctx context.Context, st *store.Store, h *hub.Hub, resendKey, mailFrom, publicURL string) (*Notifier, error) {
	n := &Notifier{st: st, hub: h, resendKey: resendKey, mailFrom: mailFrom, publicURL: publicURL, subject: "mailto:admin@localhost"}
	pub, err1 := st.GetKV(ctx, "vapid_public")
	priv, err2 := st.GetKV(ctx, "vapid_private")
	if err1 != nil || err2 != nil {
		priv, pub, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			return nil, err
		}
		_ = st.SetKV(ctx, "vapid_public", pub)
		_ = st.SetKV(ctx, "vapid_private", priv)
		n.vapidPub, n.vapidPriv = pub, priv
	} else {
		n.vapidPub, n.vapidPriv = pub, priv
	}
	return n, nil
}

func (n *Notifier) VAPIDPublicKey() string { return n.vapidPub }

func (n *Notifier) EmailEnabled() bool { return n.resendKey != "" && n.mailFrom != "" }

// Notify records an in-app notification, pushes it to subscribed devices and,
// when requested and configured, emails the user. ref says what it is about
// (e.g. "report:<id>"), for opening it from the notification.
func (n *Notifier) Notify(ctx context.Context, u *store.User, ref, title, body string, email bool) error {
	rec, err := n.st.AddNotification(ctx, u.ID, ref, title, body)
	if err != nil {
		return err
	}
	n.hub.Publish(u.ID, hub.Event{Type: "notification", Data: rec})
	go n.push(u.ID, ref, title, body)
	if email && n.EmailEnabled() && u.Email != "" {
		if err := n.sendEmail(ctx, u.Email, title, body, ref); err != nil {
			return fmt.Errorf("in-app notification sent, email failed: %w", err)
		}
	}
	return nil
}

func (n *Notifier) push(userID int64, ref, title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	subs, err := n.st.PushSubs(ctx, userID)
	if err != nil || len(subs) == 0 {
		return
	}
	payload, _ := json.Marshal(map[string]string{"title": title, "body": body, "ref": ref})
	for _, js := range subs {
		var sub webpush.Subscription
		if json.Unmarshal([]byte(js), &sub) != nil {
			continue
		}
		resp, err := webpush.SendNotificationWithContext(ctx, payload, &sub, &webpush.Options{
			Subscriber:      n.subject,
			VAPIDPublicKey:  n.vapidPub,
			VAPIDPrivateKey: n.vapidPriv,
			TTL:             3600,
			Urgency:         webpush.UrgencyHigh,
		})
		if err != nil {
			slog.Warn("web push failed", "err", err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
			_ = n.st.DeletePushSub(ctx, sub.Endpoint)
		}
	}
}

func (n *Notifier) sendEmail(ctx context.Context, to, subject, body, ref string) error {
	text := body
	if n.publicURL != "" {
		text += "\n\n" + n.publicURL + openPath(ref)
	}
	payload, _ := json.Marshal(map[string]any{
		"from":    n.mailFrom,
		"to":      []string{to},
		"subject": subject,
		"text":    text,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+n.resendKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("email provider returned %s", resp.Status)
	}
	return nil
}

// openPath is the app URL that shows what a notification is about.
func openPath(ref string) string {
	if id, ok := strings.CutPrefix(ref, "report:"); ok {
		return "/?report=" + id
	}
	return "/"
}
