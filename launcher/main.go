package main

import (
	"html/template"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	maxServices = 2
	port        = ":8090"
)

// hostToService maps each domain to its service name
var hostToService = map[string]string{
	"marimo.soylideria.com":       "marimo",
	"n8ntest.soylideria.com":      "n8n",
	"bolttest.soylideria.com":     "bolt",
	"chatwoottest.soylideria.com": "chatwoot",
	"chat.soylideria.com":         "librechat",
}

// companions are services that start/stop together
var companions = map[string][]string{
	"chatwoot": {"chatwoot_sidekiq"},
}

type serviceState struct {
	startedAt time.Time
	starting  bool
}

var (
	mu     sync.Mutex
	active = map[string]*serviceState{}
)

var waitPage = template.Must(template.New("wait").Parse(`<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <meta http-equiv="refresh" content="6">
    <title>Iniciando {{.Service}}...</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
            background: #f0f2f5;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
        }
        .card {
            background: white;
            padding: 48px 64px;
            border-radius: 16px;
            box-shadow: 0 4px 24px rgba(0,0,0,0.08);
            text-align: center;
            max-width: 420px;
            width: 90%;
        }
        .spinner {
            width: 48px; height: 48px;
            border: 4px solid #e9ecef;
            border-top-color: #007bff;
            border-radius: 50%;
            animation: spin 0.9s linear infinite;
            margin: 0 auto 24px;
        }
        @keyframes spin { to { transform: rotate(360deg); } }
        h2 { font-size: 1.4rem; color: #1a1a2e; margin-bottom: 8px; }
        p { color: #6c757d; font-size: 0.9rem; margin-top: 8px; }
        .service { color: #007bff; font-weight: 600; }
    </style>
</head>
<body>
    <div class="card">
        <div class="spinner"></div>
        <h2>Iniciando <span class="service">{{.Service}}</span></h2>
        <p>Esta página se actualizará automáticamente cada 6 segundos.</p>
    </div>
</body>
</html>`))

func main() {
	http.HandleFunc("/", handleRequest)
	log.Printf("Launcher escuchando en %s", port)
	log.Fatal(http.ListenAndServe(port, nil))
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}

	service, ok := hostToService[host]
	if !ok {
		http.Error(w, "Servicio no encontrado", http.StatusNotFound)
		return
	}

	go triggerStart(service)
	renderWait(w, service)
}

func triggerStart(service string) {
	mu.Lock()

	// Si ya está corriendo o iniciando, no hacer nada
	if state, exists := active[service]; exists && state.starting {
		mu.Unlock()
		return
	}
	if isRunning(service) {
		mu.Unlock()
		return
	}

	// Si hay maxServices activos, apagar el más antiguo
	running := getRunningServices()
	if len(running) >= maxServices {
		oldest := findOldest(running)
		mu.Unlock()
		log.Printf("Límite alcanzado — deteniendo %s (más antiguo)", oldest)
		stopService(oldest)
		mu.Lock()
		delete(active, oldest)
	}

	active[service] = &serviceState{startedAt: time.Now(), starting: true}
	mu.Unlock()

	log.Printf("Iniciando %s...", service)
	startService(service)

	mu.Lock()
	if state, exists := active[service]; exists {
		state.starting = false
	}
	mu.Unlock()
	log.Printf("%s iniciado", service)
}

func isRunning(service string) bool {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", service).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func getRunningServices() []string {
	var running []string
	for svc := range active {
		if isRunning(svc) {
			running = append(running, svc)
		}
	}
	return running
}

func findOldest(running []string) string {
	var oldest string
	var oldestTime time.Time
	for _, svc := range running {
		if state, ok := active[svc]; ok {
			if oldest == "" || state.startedAt.Before(oldestTime) {
				oldest = svc
				oldestTime = state.startedAt
			}
		}
	}
	return oldest
}

func startService(service string) {
	targets := []string{service}
	if extra, ok := companions[service]; ok {
		targets = append(targets, extra...)
	}
	args := append([]string{"start"}, targets...)
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		log.Printf("Error iniciando %s: %v\n%s", service, err, out)
	}
}

func stopService(service string) {
	targets := []string{service}
	if extra, ok := companions[service]; ok {
		targets = append(targets, extra...)
	}
	args := append([]string{"stop"}, targets...)
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		log.Printf("Error deteniendo %s: %v\n%s", service, err, out)
	}
}

func renderWait(w http.ResponseWriter, service string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	waitPage.Execute(w, map[string]string{"Service": service})
}
