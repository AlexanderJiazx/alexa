package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"alexagent/internal/agent"
)

func (s *Server) handleICS(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	rep, err := s.st.GetReport(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "report not found")
		return
	}
	var data agent.ReportData
	_ = json.Unmarshal(rep.Data, &data)
	if len(data.Events) == 0 {
		writeErr(w, 404, "no events")
		return
	}
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		loc = time.UTC
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeName(rep.Title)+`.ics"`)
	_, _ = io.WriteString(w, buildICS(data.Events, rep.ID, loc))
}

func icsEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`)
	return r.Replace(s)
}

// icsFold folds content lines at 75 octets per RFC 5545.
func icsFold(line string) string {
	if len(line) <= 75 {
		return line + "\r\n"
	}
	var b strings.Builder
	for len(line) > 75 {
		cut := 75
		for cut > 0 && (line[cut]&0xC0) == 0x80 { // don't split UTF-8 runes
			cut--
		}
		b.WriteString(line[:cut] + "\r\n ")
		line = line[cut:]
	}
	b.WriteString(line + "\r\n")
	return b.String()
}

func parseEventTime(s string, loc *time.Location) (time.Time, bool, error) {
	s = strings.TrimSpace(s)
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return t, true, nil
	}
	for _, l := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(l, s, loc); err == nil {
			return t, false, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("bad time %q", s)
}

func buildICS(events []agent.CalEvent, uidBase string, loc *time.Location) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Alexa//Personal Agent//EN\r\nCALSCALE:GREGORIAN\r\nMETHOD:PUBLISH\r\n")
	stamp := time.Now().UTC().Format("20060102T150405Z")
	for i, e := range events {
		start, allDay, err := parseEventTime(e.Start, loc)
		if err != nil {
			continue
		}
		allDay = allDay || e.AllDay
		var end time.Time
		if e.End != "" {
			end, _, err = parseEventTime(e.End, loc)
		}
		if e.End == "" || err != nil || !end.After(start) {
			if allDay {
				end = start.AddDate(0, 0, 1)
			} else {
				end = start.Add(time.Hour)
			}
		}
		b.WriteString("BEGIN:VEVENT\r\n")
		b.WriteString(icsFold(fmt.Sprintf("UID:%s-%d@alexa", uidBase, i)))
		b.WriteString("DTSTAMP:" + stamp + "\r\n")
		if allDay {
			b.WriteString("DTSTART;VALUE=DATE:" + start.Format("20060102") + "\r\n")
			b.WriteString("DTEND;VALUE=DATE:" + end.Format("20060102") + "\r\n")
		} else {
			b.WriteString("DTSTART:" + start.UTC().Format("20060102T150405Z") + "\r\n")
			b.WriteString("DTEND:" + end.UTC().Format("20060102T150405Z") + "\r\n")
		}
		b.WriteString(icsFold("SUMMARY:" + icsEscape(e.Title)))
		if e.Location != "" {
			b.WriteString(icsFold("LOCATION:" + icsEscape(e.Location)))
		}
		if e.Description != "" {
			b.WriteString(icsFold("DESCRIPTION:" + icsEscape(e.Description)))
		}
		b.WriteString("END:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}
