package agent

import "encoding/json"

// Needs describes why the session is waiting on the user.
type Needs struct {
	Kind     string   `json:"kind"` // takeover | question
	Message  string   `json:"message"`
	Options  []string `json:"options,omitempty"`
	CallID   string   `json:"callId"`
	ToolName string   `json:"toolName"`
	EntryID  int64    `json:"entryId,omitempty"` // the feed entry that shows it
}

// Surface is what the user's screen shows next to the conversation: the
// live browser, a report, or nothing. It follows what the agent is doing.
type Surface struct {
	Kind string `json:"kind"` // "" | browser | report
	ID   string `json:"id,omitempty"`
}

// ReportData is the structured part of a report, shown around its content.
type ReportData struct {
	Items   []ReportItem `json:"items,omitempty"`
	Events  []CalEvent   `json:"events,omitempty"`
	Files   []string     `json:"files,omitempty"` // workspace-relative paths
	Sources []Link       `json:"sources,omitempty"`
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
}
