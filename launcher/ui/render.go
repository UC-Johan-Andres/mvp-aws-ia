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
	tmplLogin    *template.Template
	tmplWait     *template.Template
	tmplQueue    *template.Template
	tmplFragment *template.Template
)

// fragmentTmpl uses the same CSS classes as queue.html so styles apply correctly
// after HTMX outerHTML swap (the page <style> block is still in the DOM).
const fragmentTmpl = `<div
    id="queue-status"
    hx-get="/services/queue-status"
    hx-trigger="every 3s"
    hx-swap="outerHTML">
    <h3>Servicios activos</h3>
    {{if .Running}}
        {{range .Running}}
        <div class="svc-row">
            <div class="svc-info">
                <div class="dot"></div>
                <span class="svc-label">{{.}}</span>
            </div>
            <button class="btn-stop"
                hx-post="/services/{{.}}/stop"
                hx-target="#queue-status"
                hx-swap="outerHTML"
                hx-confirm="¿Detener {{.}}?">
                Detener
            </button>
        </div>
        {{end}}
    {{else}}
        <p class="empty-msg">Ningún servicio activo.</p>
    {{end}}
    <div class="slots-info">{{len .Running}} / {{.MaxServices}} slots ocupados</div>
</div>`

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

	tmplFragment, err = template.New("fragment").Parse(fragmentTmpl)
	if err != nil {
		log.Fatalf("ui: parse fragment: %v", err)
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
func RenderQueueFragment(w http.ResponseWriter, status services.Status) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplFragment.Execute(w, status); err != nil {
		log.Printf("ui: render fragment: %v", err)
	}
}
