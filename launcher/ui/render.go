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
	tmplLogin       *template.Template
	tmplWait        *template.Template
	tmplQueue       *template.Template
	tmplFragment    *template.Template
	tmplGestion     *template.Template
	tmplInviteModal *template.Template
	tmplRows        *template.Template
	tmplContent     *template.Template
	tmplCharts      *template.Template
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

	tmplGestion, err = template.ParseFS(templateFS, "templates/gestion.html")
	if err != nil {
		log.Fatalf("ui: parse gestion.html: %v", err)
	}

	tmplInviteModal, err = template.ParseFS(templateFS, "templates/invite_modal.html")
	if err != nil {
		log.Fatalf("ui: parse invite_modal.html: %v", err)
	}

	tmplRows, err = template.ParseFS(templateFS, "templates/gestion_rows.html")
	if err != nil {
		log.Fatalf("ui: parse gestion_rows.html: %v", err)
	}

	tmplContent, err = template.ParseFS(templateFS, "templates/gestion_content.html")
	if err != nil {
		log.Fatalf("ui: parse gestion_content.html: %v", err)
	}

	tmplCharts, err = template.ParseFS(templateFS, "templates/gestion_charts.html")
	if err != nil {
		log.Fatalf("ui: parse gestion_charts.html: %v", err)
	}
}

// LoginPageData is passed to login.html (error + return path after login).
type LoginPageData struct {
	Error    string
	Redirect string // ruta relativa segura, p. ej. /gestion
}

// RenderLogin renders the login page.
func RenderLogin(w http.ResponseWriter, data LoginPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplLogin.Execute(w, data); err != nil {
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

// ─────────────────────────────────────────────────────────────────
// Gestion Dashboard
// ─────────────────────────────────────────────────────────────────

// EmpresaRowView fila de la tabla de empresas (credenciales enmascaradas).
type EmpresaRowView struct {
	Name      string
	IsDefault bool
	OpenAI    string
	Gemini    string
}

type GestionData struct {
	Tab             string
	Users           interface{}
	Error           string
	ShowInviteSent  bool
	N8NSentCount    int
	N8NQueuedCount  int
	N8NErrorCount   int
	GestionMetaJSON template.JS
	Companies       []string
	DefaultCompany  string
	EmpresaRows     []EmpresaRowView
}

type gestionShellData struct {
	Tab             string
	GestionMetaJSON template.JS
}

// RenderGestion renders the full gestion dashboard page shell.
func RenderGestion(w http.ResponseWriter, tab string, metaJSON template.JS) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(metaJSON) == 0 {
		metaJSON = template.JS(`{"companies":["default"],"defaultCompany":"default"}`)
	}
	data := gestionShellData{Tab: tab, GestionMetaJSON: metaJSON}
	if err := tmplGestion.Execute(w, data); err != nil {
		log.Printf("ui: render gestion: %v", err)
	}
}

// RenderGestionContent renders the inner content (table + form) for HTMX swaps.
// Users are fetched server-side.
func RenderGestionContent(w http.ResponseWriter, tab string, users interface{}) {
	RenderGestionContentData(w, GestionData{Tab: tab, Users: users})
}

// RenderGestionContentData renders gestión inner content with optional flash (p. ej. invitación enviada).
func RenderGestionContentData(w http.ResponseWriter, data GestionData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplContent.Execute(w, data); err != nil {
		log.Printf("ui: render gestion content: %v", err)
	}
}

// RenderGestionContentWithError renders the inner content with an error message.
func RenderGestionContentWithError(w http.ResponseWriter, tab string, errMsg string) {
	RenderGestionContentData(w, GestionData{Tab: tab, Error: errMsg, Users: []interface{}{}})
}

// RenderInviteModal renders the n8n invitation modal fragment.
func RenderInviteModal(w http.ResponseWriter, email string, usersJSON string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		Email     string
		UsersJSON string
	}{Email: email, UsersJSON: usersJSON}
	if err := tmplInviteModal.Execute(w, data); err != nil {
		log.Printf("ui: render invite modal: %v", err)
	}
}

// RenderGestionRows renders the users table rows fragment for HTMX refresh.
func RenderGestionRows(w http.ResponseWriter, tab string, users string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		Tab   string
		Users string
	}{Tab: tab, Users: users}
	if err := tmplRows.Execute(w, data); err != nil {
		log.Printf("ui: render gestion rows: %v", err)
	}
}

// RenderGestionChartsContent renders the statistics / charts fragment for HTMX.
func RenderGestionChartsContent(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplCharts.Execute(w, nil); err != nil {
		log.Printf("ui: render gestion charts: %v", err)
	}
}
