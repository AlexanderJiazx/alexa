package agent

import "encoding/json"

// Needs describes why the session is waiting on the user.
type Needs struct {
	Kind     string   `json:"kind"` // takeover | question | login
	Message  string   `json:"message"`
	Options  []string `json:"options,omitempty"`
	Site     string   `json:"site,omitempty"`     // login: the site to sign in to
	Username string   `json:"username,omitempty"` // login: the account last used there
	CallID   string   `json:"callId"`
	ToolName string   `json:"toolName"`
	EntryID  int64    `json:"entryId,omitempty"` // the feed entry that shows it
	// WP-03: the rich ask (Appendix A.4): context blocks, a form, multi-select,
	// the recommended option and the confirm card.
	Blocks      []json.RawMessage `json:"blocks,omitempty"`
	Form        *AskForm          `json:"form,omitempty"`
	Multi       bool              `json:"multi,omitempty"`
	Recommended *int              `json:"recommended,omitempty"` // index into Options
	Confirm     bool              `json:"confirm,omitempty"`
}

// Surface is what the user's screen shows next to the conversation: the
// live browser, a report, or nothing. It follows what the agent is doing.
type Surface struct {
	Kind string `json:"kind"` // "" | browser | report
	ID   string `json:"id,omitempty"`
	// WP-07 (Appendix C.1): Kind may also be "entry" (ID is the entry whose
	// blocks are expanded). Held: the user chose this, so Alexa leaves it
	// alone until the next new exchange. Follow is only ever sent by
	// clients: true shows this and lets the screen follow Alexa again,
	// instead of holding it; it's never stored.
	Held   bool `json:"held,omitempty"`
	Follow bool `json:"follow,omitempty"`
}

// ReportData is the structured part of a report, shown around its content.
type ReportData struct {
	Items   []ReportItem `json:"items,omitempty"`
	Events  []CalEvent   `json:"events,omitempty"`
	Files   []string     `json:"files,omitempty"` // workspace-relative paths
	Sources []Link       `json:"sources,omitempty"`
	// WP-03: blocks shown in the document, the one-line takeaway on its card,
	// and the card's preview.
	Blocks   []json.RawMessage `json:"blocks,omitempty"`
	Takeaway string            `json:"takeaway,omitempty"`
	Preview  *ReportPreview    `json:"preview,omitempty"`
}

type ReportItem struct {
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

type CalEvent struct {
	Title       string `json:"title"`
	Start       string `json:"start"` // RFC3339 or YYYY-MM-DD for all-day
	End         string `json:"end,omitempty"`
	AllDay      bool   `json:"allDay,omitempty"`
	Location    string `json:"location,omitempty"`
	Description string `json:"description,omitempty"`
}

type Link struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// storedItem wraps a model conversation item with replay metadata.
type storedItem struct {
	Item   json.RawMessage `json:"item"`
	Kind   string          `json:"kind,omitempty"`   // browser_state | long | user | system
	Shot   string          `json:"shot,omitempty"`   // screenshot file for function outputs
	Short  string          `json:"short,omitempty"`  // replacement text when pruned
	Attach []Attachment    `json:"attach,omitempty"` // user attachments
}

// Attachment is a file the user added to a message.
type Attachment struct {
	Name string `json:"name"`
	Path string `json:"path"` // workspace-relative
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// Message is something sent into a user's session.
type Message struct {
	Text   string
	Attach []Attachment
	Voice  bool   // the user spoke it
	Source string // user | schedule
	Label  string // schedule name
	// ClientID is the id the client sent the message with. The entry's data
	// carries it, so the client can match the echo to what it shows.
	ClientID string
	// WP-03: how the app made this message, when it wasn't typed: a tapped
	// card button (Tap says which), a submitted form (Values), a multi-select
	// or the report bridge. See Runtime.reply.
	Via    string         // "" | button | form | multi | report
	Tap    *Tap           // via button
	Values map[string]any // via form: the submitted values by field name
	// AskEntry is the question a late answer answers: one the user skipped
	// and answered afterwards, from its entry (FE-1). 0 when it answers the
	// question open now, or none.
	AskEntry int64
}

// View is the session as shown to clients.
type View struct {
	Status        string  `json:"status"` // idle | working | needs
	Activity      string  `json:"activity"`
	Needs         *Needs  `json:"needs,omitempty"`
	Surface       Surface `json:"surface"`
	Control       bool    `json:"control"`
	BrowserURL    string  `json:"browserUrl,omitempty"`
	ContextTokens int64   `json:"contextTokens"`
	Compactions   int64   `json:"compactions"`
	// WP-05/07 (Appendix B.3): the run's phase and when it started (unix ms),
	// the exchange's plan, and whether Alexa's browsing opens the browser
	// (follow mode: the screen isn't held and the run isn't in the
	// background).
	Phase        string `json:"phase,omitempty"`
	RunStartedAt int64  `json:"runStartedAt,omitempty"`
	Plan         *Plan  `json:"plan,omitempty"`
	Follow       bool   `json:"follow"`
	// Queued counts the user's messages waiting for Alexa to read them. It's
	// sent even at zero: the composer marks a queued message seen when the
	// count it was part of drops to 0 (WP-18).
	Queued int `json:"queued"`
}
