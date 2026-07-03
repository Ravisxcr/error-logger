// Package web serves the local dashboard for browsing captured events.
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"

	"github.com/ravi/error-logger/internal/sentryevent"
	"github.com/ravi/error-logger/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"add1": func(i int) int { return i + 1 },
}).ParseFS(templateFS, "templates/*.html"))

type Handler struct {
	Store *store.Store
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.handleProjects)
	mux.HandleFunc("GET /projects/{project_id}", h.handleProjectIssues)
	mux.HandleFunc("GET /events/{id}", h.handleDetail)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
}

type projectRow struct {
	ProjectID string
	Issues    int
	Events    int
	LastSeen  string
}

func (h *Handler) handleProjects(w http.ResponseWriter, r *http.Request) {
	summaries := h.Store.Projects()
	rows := make([]projectRow, len(summaries))
	for i, p := range summaries {
		rows[i] = projectRow{
			ProjectID: p.ProjectID,
			Issues:    p.Issues,
			Events:    p.Events,
			LastSeen:  p.LastSeen.Local().Format("2006-01-02 15:04:05"),
		}
	}

	data := struct {
		Count    int
		Projects []projectRow
	}{Count: len(rows), Projects: rows}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "projects", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type eventRow struct {
	ID        string
	Time      string
	Level     string
	ProjectID string
	Kind      string
	Summary   string
	Count     int
}

func (h *Handler) handleProjectIssues(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project_id")
	captured := h.Store.ListByProject(projectID)
	rows := make([]eventRow, len(captured))
	for i, c := range captured {
		rows[i] = summarize(c)
	}

	data := struct {
		ProjectID string
		Count     int
		Events    []eventRow
	}{ProjectID: projectID, Count: len(rows), Events: rows}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "list", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type codeLine struct {
	Num     int
	Text    string
	Current bool
}

type kv struct {
	Key   string
	Value string
}

type frameView struct {
	Function string
	Module   string
	Location string
	InApp    bool
	Lines    []codeLine
	Vars     []kv
}

type mechanismView struct {
	Type    string
	Handled string // "handled", "unhandled", or "" if unknown
}

type exceptionView struct {
	Type      string
	Value     string
	Module    string
	Mechanism *mechanismView
	Frames    []frameView
}

type contextGroup struct {
	Name   string
	Fields []kv
}

type breadcrumbView struct {
	Time     string
	Category string
	Message  string
}

type detailView struct {
	eventRow
	FirstSeen   string
	Environment string
	Release     string
	ServerName  string
	Platform    string
	SDK         string

	Exceptions  []exceptionView
	Message     string
	Tags        map[string]string
	Breadcrumbs []breadcrumbView
	User        []kv
	Request     []kv
	Contexts    []contextGroup
	Extra       []kv
	Modules     []kv

	RawJSON string
}

func (h *Handler) handleDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, ok := h.Store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	dv := detailView{eventRow: summarize(c), FirstSeen: c.ReceivedAt.Local().Format("2006-01-02 15:04:05")}

	raw, _ := json.MarshalIndent(c, "", "  ")
	dv.RawJSON = string(raw)

	if e := c.Event; e != nil {
		dv.Environment = e.Environment
		dv.Release = e.Release
		dv.ServerName = e.ServerName
		dv.Platform = e.Platform
		if e.SDK != nil {
			dv.SDK = strings.TrimSpace(e.SDK.Name + " " + e.SDK.Version)
		}
		dv.Tags = e.Tags

		if e.Exception != nil {
			for _, exc := range e.Exception.Values {
				ev := exceptionView{Type: exc.Type, Value: exc.Value, Module: exc.Module}
				if exc.Mechanism != nil {
					mv := &mechanismView{Type: exc.Mechanism.Type}
					if exc.Mechanism.Handled != nil {
						if *exc.Mechanism.Handled {
							mv.Handled = "handled"
						} else {
							mv.Handled = "unhandled"
						}
					}
					ev.Mechanism = mv
				}
				if exc.Stacktrace != nil {
					for _, f := range exc.Stacktrace.Frames {
						ev.Frames = append(ev.Frames, buildFrame(f))
					}
				}
				dv.Exceptions = append(dv.Exceptions, ev)
			}
		} else if e.Message != nil {
			dv.Message = e.Message.Text()
		}

		if e.Breadcrumbs != nil {
			for _, b := range e.Breadcrumbs.Values {
				dv.Breadcrumbs = append(dv.Breadcrumbs, breadcrumbView{
					Time:     string(b.Timestamp),
					Category: b.Category,
					Message:  b.Message,
				})
			}
		}

		if e.User != nil {
			dv.User = compactKV([]kv{
				{"id", e.User.ID},
				{"email", e.User.Email},
				{"username", e.User.Username},
				{"ip_address", e.User.IPAddress},
			})
		}

		if e.Request != nil {
			dv.Request = compactKV([]kv{
				{"method", e.Request.Method},
				{"url", e.Request.URL},
				{"query_string", e.Request.QueryString},
			})
		}

		for _, name := range sortedKeys(e.Contexts) {
			if m, ok := e.Contexts[name].(map[string]interface{}); ok {
				dv.Contexts = append(dv.Contexts, contextGroup{Name: name, Fields: toKV(m)})
			}
		}

		dv.Extra = toKV(e.Extra)

		if len(e.Modules) > 0 {
			names := make([]string, 0, len(e.Modules))
			for n := range e.Modules {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				dv.Modules = append(dv.Modules, kv{Key: n, Value: e.Modules[n]})
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "detail", dv); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func buildFrame(f sentryevent.Frame) frameView {
	loc := f.Filename
	if loc == "" {
		loc = f.AbsPath
	}
	location := loc
	if f.Lineno != 0 {
		location = fmt.Sprintf("%s:%d", loc, f.Lineno)
	}

	fv := frameView{
		Function: orUnknown(f.Function),
		Module:   f.Module,
		Location: location,
		InApp:    f.InApp,
	}

	if f.ContextLine != "" || len(f.PreContext) > 0 || len(f.PostContext) > 0 {
		start := f.Lineno - len(f.PreContext)
		for i, t := range f.PreContext {
			fv.Lines = append(fv.Lines, codeLine{Num: start + i, Text: t})
		}
		fv.Lines = append(fv.Lines, codeLine{Num: f.Lineno, Text: f.ContextLine, Current: true})
		for i, t := range f.PostContext {
			fv.Lines = append(fv.Lines, codeLine{Num: f.Lineno + 1 + i, Text: t})
		}
	}

	if len(f.Vars) > 0 {
		var vars map[string]interface{}
		if err := json.Unmarshal(f.Vars, &vars); err == nil {
			fv.Vars = toKV(vars)
		}
	}

	return fv
}

// toKV renders a JSON object as a sorted, display-ready key/value list.
func toKV(m map[string]interface{}) []kv {
	out := make([]kv, 0, len(m))
	for _, k := range sortedKeys(m) {
		out = append(out, kv{Key: k, Value: stringify(m[k])})
	}
	return out
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringify(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case nil:
		return "null"
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprint(val)
		}
		return string(b)
	}
}

// compactKV drops empty-valued pairs so the template doesn't render blank rows.
func compactKV(pairs []kv) []kv {
	out := make([]kv, 0, len(pairs))
	for _, p := range pairs {
		if p.Value != "" {
			out = append(out, p)
		}
	}
	return out
}

func summarize(c store.Captured) eventRow {
	row := eventRow{
		ID:        c.ID,
		Time:      c.LastSeen.Local().Format("2006-01-02 15:04:05"),
		ProjectID: c.ProjectID,
		Kind:      c.Kind,
		Level:     "info",
		Summary:   c.Kind,
		Count:     c.Count,
	}

	if c.Event == nil {
		return row
	}

	if c.Event.Level != "" {
		row.Level = strings.ToLower(c.Event.Level)
	} else if c.Kind == "event" {
		row.Level = "error"
	}

	switch {
	case c.Event.Exception != nil && len(c.Event.Exception.Values) > 0:
		exc := c.Event.Exception.Values[len(c.Event.Exception.Values)-1]
		row.Summary = fmt.Sprintf("%s: %s", exc.Type, exc.Value)
	case c.Event.Message != nil && c.Event.Message.Text() != "":
		row.Summary = c.Event.Message.Text()
	case c.Event.Transaction != "":
		row.Summary = c.Event.Transaction
	}

	return row
}

func orUnknown(s string) string {
	if s == "" {
		return "<unknown>"
	}
	return s
}
