package api

import (
	"io"
	"net/http"

	"alexagent/internal/agent"
	"alexagent/internal/hub"
	"alexagent/internal/store"
)

func (s *Server) handleListReports(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.ListReports(r.Context(), userFrom(r).ID, 200)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if list == nil {
		list = []*store.Report{}
	}
	writeJSON(w, list)
}

func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	rep, err := s.st.GetReport(r.Context(), userFrom(r).ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "report not found")
		return
	}
	writeJSON(w, rep)
}

func (s *Server) handleDeleteReport(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	ok, err := s.st.DeleteReport(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if !ok {
		writeErr(w, 404, "report not found")
		return
	}
	if v := s.rt.View(r.Context(), u.ID); v.Surface.Kind == "report" && v.Surface.ID == r.PathValue("id") {
		_ = s.rt.SetSurface(r.Context(), u, agent.Surface{})
	}
	s.hub.Publish(u.ID, hub.ChangedItem("reportDeleted", r.PathValue("id")))
	writeJSON(w, map[string]bool{"ok": true})
}

// handleReportDownload saves a report as a markdown or HTML file.
func (s *Server) handleReportDownload(w http.ResponseWriter, r *http.Request) {
	rep, err := s.st.GetReport(r.Context(), userFrom(r).ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "report not found")
		return
	}
	ext, ct, body := ".md", "text/markdown; charset=utf-8", s.rt.ReportMarkdown(userFrom(r).ID, rep)
	if rep.Format == "html" {
		ext, ct, body = ".html", "text/html; charset=utf-8", rep.Content
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeName(rep.Title)+ext+`"`)
	_, _ = io.WriteString(w, body)
}
