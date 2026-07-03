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

	"github.com/ravisxcr/error-logger/internal/sentryevent"
	"github.com/ravisxcr/error-logger/internal/store"
)

//go:embed templates
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// baseTemplate holds the shared layout and partials (head, header, icons,
// theme scripts). Each page clones it and adds its own "content" definition,
// mirroring an extends/block template inheritance model.
var baseTemplate = template.Must(template.New("").Funcs(template.FuncMap{
	"add1": func(i int) int { return i + 1 },
}).ParseFS(templateFS, "templates/layout.html", "templates/partials/*.html"))

func pageTemplate(page string) *template.Template {
	return template.Must(template.Must(baseTemplate.Clone()).ParseFS(templateFS, "templates/pages/"+page))
}

var (
	projectsTmpl = pageTemplate("projects.html")
	listTmpl     = pageTemplate("list.html")
	detailTmpl   = pageTemplate("detail.html")
)

// Page is the data every template execution starts from: shared chrome
// (title, back link, subtitle, refresh) plus the page-specific Data payload.
type Page struct {
	Title       string
	Subtitle    string
	BackHref    string
	BackLabel   string
	AutoRefresh bool
	Data        interface{}
}

type Handler struct {
	Store *store.Store
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.handleProjects)
	mux.HandleFunc("GET /projects/{project_id}", h.handleProjectIssues)
	mux.HandleFunc("POST /projects/{project_id}/delete", h.handleDeleteProject)
	mux.HandleFunc("GET /events/{id}", h.handleDetail)
	mux.HandleFunc("POST /events/{id}/delete", h.handleDeleteEvent)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
}

type projectRow struct {
	ProjectID string
	Issues    int
	Events    int
	LastSeen  string
}

func (h *Handler) handleProjects(writer http.ResponseWriter, request *http.Request) {
	summaries := h.Store.Projects()
	rows := make([]projectRow, len(summaries))
	for index, summary := range summaries {
		rows[index] = projectRow{
			ProjectID: summary.ProjectID,
			Issues:    summary.Issues,
			Events:    summary.Events,
			LastSeen:  summary.LastSeen.Local().Format("2006-01-02 15:04:05"),
		}
	}

	page := Page{
		Title:       "error-logger",
		Subtitle:    pluralize(len(rows), "project"),
		AutoRefresh: true,
		Data:        struct{ Projects []projectRow }{Projects: rows},
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := projectsTmpl.ExecuteTemplate(writer, "layout", page); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
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

func (h *Handler) handleProjectIssues(writer http.ResponseWriter, request *http.Request) {
	projectID := request.PathValue("project_id")
	captured := h.Store.ListByProject(projectID)
	rows := make([]eventRow, len(captured))
	for index, capturedEvent := range captured {
		rows[index] = summarize(capturedEvent)
	}

	page := Page{
		Title:       fmt.Sprintf("%s — error-logger", projectID),
		Subtitle:    fmt.Sprintf("%s in %s", pluralize(len(rows), "issue"), projectID),
		BackHref:    "/",
		BackLabel:   "error-logger",
		AutoRefresh: true,
		Data:        struct{ Events []eventRow }{Events: rows},
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := listTmpl.ExecuteTemplate(writer, "layout", page); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
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

func (h *Handler) handleDetail(writer http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	capturedEvent, ok := h.Store.Get(id)
	if !ok {
		http.NotFound(writer, request)
		return
	}

	detailViewData := buildDetailView(capturedEvent)

	page := Page{
		Title:     fmt.Sprintf("%s — error-logger", detailViewData.Summary),
		BackHref:  "/projects/" + detailViewData.ProjectID,
		BackLabel: detailViewData.ProjectID,
		Data:      detailViewData,
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := detailTmpl.ExecuteTemplate(writer, "layout", page); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

func buildDetailView(capturedEvent store.Captured) detailView {
	detailViewData := detailView{
		eventRow:  summarize(capturedEvent),
		FirstSeen: capturedEvent.ReceivedAt.Local().Format("2006-01-02 15:04:05"),
	}

	raw, _ := json.MarshalIndent(capturedEvent, "", "  ")
	detailViewData.RawJSON = string(raw)

	eventData := capturedEvent.Event
	if eventData == nil {
		return detailViewData
	}

	detailViewData.Environment = eventData.Environment
	detailViewData.Release = eventData.Release
	detailViewData.ServerName = eventData.ServerName
	detailViewData.Platform = eventData.Platform
	if eventData.SDK != nil {
		detailViewData.SDK = strings.TrimSpace(eventData.SDK.Name + " " + eventData.SDK.Version)
	}
	detailViewData.Tags = eventData.Tags

	detailViewData.Exceptions = buildExceptions(eventData)
	if len(detailViewData.Exceptions) == 0 {
		if msg := eventData.MessageText(); msg != "" {
			detailViewData.Message = msg
		}
	}

	detailViewData.Breadcrumbs = buildBreadcrumbs(eventData)
	detailViewData.User = buildUser(eventData)
	detailViewData.Request = buildRequest(eventData)
	detailViewData.Contexts = buildContexts(eventData)
	detailViewData.Extra = toKV(eventData.Extra)
	detailViewData.Modules = buildModules(eventData)

	return detailViewData
}

func buildExceptions(eventData *sentryevent.Event) []exceptionView {
	if eventData.Exception == nil {
		return nil
	}
	var exceptionsList []exceptionView
	for _, exceptionVal := range eventData.Exception.Values {
		exceptionViewData := exceptionView{Type: exceptionVal.Type, Value: exceptionVal.Value, Module: exceptionVal.Module}
		if exceptionVal.Mechanism != nil {
			mechanismViewData := &mechanismView{Type: exceptionVal.Mechanism.Type}
			if exceptionVal.Mechanism.Handled != nil {
				if *exceptionVal.Mechanism.Handled {
					mechanismViewData.Handled = "handled"
				} else {
					mechanismViewData.Handled = "unhandled"
				}
			}
			exceptionViewData.Mechanism = mechanismViewData
		}
		if exceptionVal.Stacktrace != nil {
			for _, frameVal := range exceptionVal.Stacktrace.Frames {
				exceptionViewData.Frames = append(exceptionViewData.Frames, buildFrame(frameVal))
			}
		}
		exceptionsList = append(exceptionsList, exceptionViewData)
	}
	return exceptionsList
}

func buildBreadcrumbs(eventData *sentryevent.Event) []breadcrumbView {
	if eventData.Breadcrumbs == nil {
		return nil
	}
	var breadcrumbsList []breadcrumbView
	for _, breadcrumbVal := range eventData.Breadcrumbs.Values {
		breadcrumbsList = append(breadcrumbsList, breadcrumbView{
			Time:     breadcrumbVal.Time().Local().Format("15:04:05"),
			Category: breadcrumbVal.Category,
			Message:  breadcrumbVal.Message,
		})
	}
	return breadcrumbsList
}

func buildUser(eventData *sentryevent.Event) []kv {
	if eventData.User == nil {
		return nil
	}
	return compactKV([]kv{
		{"id", eventData.User.ID},
		{"email", eventData.User.Email},
		{"username", eventData.User.Username},
		{"ip_address", eventData.User.IPAddress},
	})
}

func buildRequest(eventData *sentryevent.Event) []kv {
	if eventData.Request == nil {
		return nil
	}
	return compactKV([]kv{
		{"method", eventData.Request.Method},
		{"url", eventData.Request.URL},
		{"query_string", eventData.Request.QueryString},
	})
}

func buildContexts(eventData *sentryevent.Event) []contextGroup {
	var contextGroupsList []contextGroup
	for _, name := range sortedKeys(eventData.Contexts) {
		if contextMap, ok := eventData.Contexts[name].(map[string]interface{}); ok {
			contextGroupsList = append(contextGroupsList, contextGroup{Name: name, Fields: toKV(contextMap)})
		}
	}
	return contextGroupsList
}

func buildModules(eventData *sentryevent.Event) []kv {
	if len(eventData.Modules) == 0 {
		return nil
	}
	names := make([]string, 0, len(eventData.Modules))
	for moduleName := range eventData.Modules {
		names = append(names, moduleName)
	}
	sort.Strings(names)
	var modulesList []kv
	for _, moduleName := range names {
		modulesList = append(modulesList, kv{Key: moduleName, Value: eventData.Modules[moduleName]})
	}
	return modulesList
}

// handleDeleteEvent deletes a single issue (and, since grouped occurrences
// share an ID, its full occurrence history) and returns to the project's
// issue list.
func (h *Handler) handleDeleteEvent(writer http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	capturedEvent, ok := h.Store.Get(id)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	if _, err := h.Store.DeleteEvent(id); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(writer, request, "/projects/"+capturedEvent.ProjectID, http.StatusSeeOther)
}

// handleDeleteProject deletes every captured row for a project and returns
// to the project overview.
func (h *Handler) handleDeleteProject(writer http.ResponseWriter, request *http.Request) {
	projectID := request.PathValue("project_id")
	if _, err := h.Store.DeleteProject(projectID); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(writer, request, "/", http.StatusSeeOther)
}

func buildFrame(frameData sentryevent.Frame) frameView {
	locationPath := frameData.Filename
	if locationPath == "" {
		locationPath = frameData.AbsPath
	}
	location := locationPath
	if frameData.Lineno != 0 {
		location = fmt.Sprintf("%s:%d", locationPath, frameData.Lineno)
	}

	frameViewData := frameView{
		Function: orUnknown(frameData.Function),
		Module:   frameData.Module,
		Location: location,
		InApp:    frameData.InApp,
	}

	if frameData.ContextLine != "" || len(frameData.PreContext) > 0 || len(frameData.PostContext) > 0 {
		start := frameData.Lineno - len(frameData.PreContext)
		for lineIndex, contextText := range frameData.PreContext {
			frameViewData.Lines = append(frameViewData.Lines, codeLine{Num: start + lineIndex, Text: contextText})
		}
		frameViewData.Lines = append(frameViewData.Lines, codeLine{Num: frameData.Lineno, Text: frameData.ContextLine, Current: true})
		for lineIndex, contextText := range frameData.PostContext {
			frameViewData.Lines = append(frameViewData.Lines, codeLine{Num: frameData.Lineno + 1 + lineIndex, Text: contextText})
		}
	}

	if len(frameData.Vars) > 0 {
		var vars map[string]interface{}
		if err := json.Unmarshal(frameData.Vars, &vars); err == nil {
			frameViewData.Vars = toKV(vars)
		}
	}

	return frameViewData
}

// toKV renders a JSON object as a sorted, display-ready key/value list.
func toKV(dataMap map[string]interface{}) []kv {
	kvList := make([]kv, 0, len(dataMap))
	for _, mapKey := range sortedKeys(dataMap) {
		kvList = append(kvList, kv{Key: mapKey, Value: stringify(dataMap[mapKey])})
	}
	return kvList
}

func sortedKeys(dataMap map[string]interface{}) []string {
	keys := make([]string, 0, len(dataMap))
	for mapKey := range dataMap {
		keys = append(keys, mapKey)
	}
	sort.Strings(keys)
	return keys
}

func stringify(value interface{}) string {
	switch typedValue := value.(type) {
	case string:
		return typedValue
	case nil:
		return "null"
	default:
		jsonBytes, err := json.Marshal(typedValue)
		if err != nil {
			return fmt.Sprint(typedValue)
		}
		return string(jsonBytes)
	}
}

// compactKV drops empty-valued pairs so the template doesn't render blank rows.
func compactKV(pairs []kv) []kv {
	compactedList := make([]kv, 0, len(pairs))
	for _, pair := range pairs {
		if pair.Value != "" {
			compactedList = append(compactedList, pair)
		}
	}
	return compactedList
}

func summarize(capturedEvent store.Captured) eventRow {
	row := eventRow{
		ID:        capturedEvent.ID,
		Time:      capturedEvent.LastSeen.Local().Format("2006-01-02 15:04:05"),
		ProjectID: capturedEvent.ProjectID,
		Kind:      capturedEvent.Kind,
		Level:     "info",
		Summary:   capturedEvent.Kind,
		Count:     capturedEvent.Count,
	}

	if capturedEvent.Event == nil {
		return row
	}

	if capturedEvent.Event.Level != "" {
		row.Level = strings.ToLower(capturedEvent.Event.Level)
	} else if capturedEvent.Kind == "event" {
		row.Level = "error"
	}

	switch {
	case capturedEvent.Event.Exception != nil && len(capturedEvent.Event.Exception.Values) > 0:
		exceptionVal := capturedEvent.Event.Exception.Values[len(capturedEvent.Event.Exception.Values)-1]
		row.Summary = fmt.Sprintf("%s: %s", exceptionVal.Type, exceptionVal.Value)
	case capturedEvent.Event.MessageText() != "":
		row.Summary = capturedEvent.Event.MessageText()
	case capturedEvent.Event.Transaction != "":
		row.Summary = capturedEvent.Event.Transaction
	}

	return row
}

func pluralize(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %ss", count, singular)
}

func orUnknown(inputStr string) string {
	if inputStr == "" {
		return "<unknown>"
	}
	return inputStr
}
