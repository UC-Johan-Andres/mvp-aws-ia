package ui

import (
	"embed"
	"html/template"
	"log"
	"net/http"

	"launcher/services"
)

//go:embed templates/*.html
var templateFS embed.FS

var (
	tmplLogin *template.Template
	tmplWait  *template.Template
	tmplQueue *template.Template
)

func init() {
	var err error

	tmplLogin, err = template.ParseFS(templateFS, "templates/login.html")
	if err != nil {
		log.Fatalf("ui: parse login.html: %v", err)
	}

	tmplWait, err = template.ParseFS(templateFS, "templates/wait.html")
	if err != nil {
		log.Fatalf("ui: parse wait.html: %v", err)
	}

	tmplQueue, err = template.ParseFS(templateFS, "templates/queue.html")
	if err != nil {
		log.Fatalf("ui: parse queue.html: %v", err)
	}
}

// RenderLogin renders the login page with an optional error message.
func RenderLogin(w http.ResponseWriter, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplLogin.Execute(w, map[string]string{"Error": errMsg}); err != nil {
		log.Printf("ui: render login: %v", err)
	}
}

// RenderWait renders the wait page for the given service.
func RenderWait(w http.ResponseWriter, service string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct{ Service string }{Service: service}
	if err := tmplWait.Execute(w, data); err != nil {
		log.Printf("ui: render wait: %v", err)
	}
}

// queuePageData is the template data for queue.html.
type queuePageData struct {
	Service  string
	Position int
	Status   services.Status
}

// RenderQueue renders the full queue page showing the service's queue position
// and the list of currently running services.
func RenderQueue(w http.ResponseWriter, service string, position int, status services.Status) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := queuePageData{
		Service:  service,
		Position: position,
		Status:   status,
	}
	if err := tmplQueue.Execute(w, data); err != nil {
		log.Printf("ui: render queue: %v", err)
	}
}

// RenderQueueFragment renders only the #queue-status div fragment for HTMX swaps.
// It reuses the queue template but only executes the "queue-status" block.
func RenderQueueFragment(w http.ResponseWriter, status services.Status) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Inline fragment template that matches the #queue-status structure in queue.html.
	// This is kept separate so HTMX outerHTML swap replaces exactly that element.
	const fragmentTmpl = `<div
    id="queue-status"
    hx-get="/services/queue-status"
    hx-trigger="every 3s"
    hx-swap="outerHTML">

    <h3 style="font-size:0.85rem;text-transform:uppercase;letter-spacing:0.05em;color:#6c757d;margin-bottom:12px;">Servicios activos</h3>
    {{if .Running}}
        {{range .Running}}
        <div style="display:flex;align-items:center;justify-content:space-between;padding:8px 0;border-bottom:1px solid #f1f3f5;">
            <div style="display:flex;align-items:center;gap:10px;">
                <div style="width:8px;height:8px;border-radius:50%;background:#28a745;flex-shrink:0;"></div>
                <span style="font-size:0.95rem;color:#212529;font-weight:500;">{{.}}</span>
            </div>
            <button
                style="padding:5px 14px;background:#dc3545;color:white;border:none;border-radius:6px;font-size:0.8rem;cursor:pointer;"
                hx-post="/services/{{.}}/stop"
                hx-target="#queue-status"
                hx-swap="outerHTML">
                Detener
            </button>
        </div>
        {{end}}
    {{else}}
        <p style="color:#6c757d;font-size:0.9rem;font-style:italic;">Ningún servicio activo.</p>
    {{end}}

    <div style="margin-top:12px;font-size:0.8rem;color:#6c757d;text-align:right;">
        {{len .Running}} / {{.MaxServices}} slots ocupados
    </div>
</div>`

	tmpl, err := template.New("fragment").Parse(fragmentTmpl)
	if err != nil {
		log.Printf("ui: parse fragment: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, status); err != nil {
		log.Printf("ui: render fragment: %v", err)
	}
}
