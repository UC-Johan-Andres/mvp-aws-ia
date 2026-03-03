package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxServices = 2
	port        = ":8090"
	cookieName  = "bolt_session"
	cookieTTL   = 24 * time.Hour
)

var (
	authUser      = getEnv("AUTH_USER", "admin")
	authPassword  = getEnv("AUTH_PASSWORD", "")
	sessionSecret = getEnv("SESSION_SECRET", "change_me")
	agentSocket   = getEnv("AGENT_SOCKET", "/var/run/docker-agent.sock")
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

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

var loginPage = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <title>Acceso — Bolt.diy</title>
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
            max-width: 380px;
            width: 90%;
        }
        h2 { font-size: 1.4rem; color: #1a1a2e; margin-bottom: 28px; }
        .field { margin-bottom: 16px; text-align: left; }
        label { display: block; font-size: 0.85rem; color: #6c757d; margin-bottom: 6px; }
        input {
            width: 100%;
            padding: 10px 14px;
            border: 1px solid #dee2e6;
            border-radius: 8px;
            font-size: 0.95rem;
            outline: none;
            transition: border-color 0.15s;
        }
        input:focus { border-color: #007bff; }
        button {
            width: 100%;
            padding: 11px;
            background: #007bff;
            color: white;
            border: none;
            border-radius: 8px;
            font-size: 1rem;
            cursor: pointer;
            margin-top: 8px;
            transition: background 0.15s;
        }
        button:hover { background: #0056b3; }
        .error { color: #dc3545; font-size: 0.85rem; margin-bottom: 16px; }
    </style>
</head>
<body>
    <div class="card">
        <h2>Bolt.diy</h2>
        {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
        <form method="POST" action="/auth/login">
            <div class="field">
                <label>Usuario</label>
                <input type="text" name="user" autofocus autocomplete="username">
            </div>
            <div class="field">
                <label>Contraseña</label>
                <input type="password" name="pass" autocomplete="current-password">
            </div>
            <button type="submit">Entrar</button>
        </form>
    </div>
</body>
</html>`))

func main() {
	http.HandleFunc("/auth/check", handleAuthCheck)
	http.HandleFunc("/auth/login", handleAuthLogin)
	http.HandleFunc("/auth/logout", handleAuthLogout)
	http.HandleFunc("/", handleRequest)
	log.Printf("Launcher escuchando en %s", port)
	log.Fatal(http.ListenAndServe(port, nil))
}

// --- Auth handlers ---

func handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || !validToken(cookie.Value) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.ParseForm()
		user := r.FormValue("user")
		pass := r.FormValue("pass")
		if authPassword != "" && user == authUser && pass == authPassword {
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    makeToken(),
				Path:     "/",
				Domain:   ".soylideria.com",
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   int(cookieTTL.Seconds()),
			})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		renderLogin(w, "Credenciales incorrectas")
		return
	}
	renderLogin(w, "")
}

func handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:   cookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
}

func renderLogin(w http.ResponseWriter, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	loginPage.Execute(w, map[string]string{"Error": errMsg})
}

// --- Token helpers (HMAC-SHA256, stateless) ---

func makeToken() string {
	exp := fmt.Sprintf("%d", time.Now().Add(cookieTTL).Unix())
	return exp + "." + sign(exp)
}

func validToken(tok string) bool {
	dot := strings.LastIndex(tok, ".")
	if dot < 0 {
		return false
	}
	payload, sig := tok[:dot], tok[dot+1:]
	if !hmac.Equal([]byte(sign(payload)), []byte(sig)) {
		return false
	}
	exp, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < exp
}

func sign(data string) string {
	h := hmac.New(sha256.New, []byte(sessionSecret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// --- Service launcher handlers ---

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

	// Si hay maxServices activos, determinar cuál apagar
	var toEvict string
	running := getRunningServices()
	if len(running) >= maxServices {
		toEvict = findOldest(running)
		delete(active, toEvict)
	}

	// Registrar el servicio como "iniciando" antes de soltar el mutex
	active[service] = &serviceState{startedAt: time.Now(), starting: true}
	mu.Unlock()

	// Operaciones lentas fuera del lock
	if toEvict != "" {
		log.Printf("Límite alcanzado — deteniendo %s (más antiguo)", toEvict)
		stopService(toEvict)
	}

	log.Printf("Iniciando %s...", service)
	startService(service)

	mu.Lock()
	if state, exists := active[service]; exists {
		state.starting = false
	}
	mu.Unlock()
	log.Printf("%s iniciado", service)
}

func agentCall(command string) (string, error) {
	conn, err := net.Dial("unix", agentSocket)
	if err != nil {
		return "", fmt.Errorf("dial agent: %w", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "%s\n", command)
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return "", fmt.Errorf("no response from agent")
	}
	return strings.TrimSpace(scanner.Text()), nil
}

func isRunning(service string) bool {
	resp, err := agentCall("STATUS " + service)
	return err == nil && resp == "RUNNING"
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
	resp, err := agentCall("START " + strings.Join(targets, " "))
	if err != nil || resp != "OK" {
		log.Printf("Error iniciando %s: %v %s", service, err, resp)
	}
}

func stopService(service string) {
	targets := []string{service}
	if extra, ok := companions[service]; ok {
		targets = append(targets, extra...)
	}
	resp, err := agentCall("STOP " + strings.Join(targets, " "))
	if err != nil || resp != "OK" {
		log.Printf("Error deteniendo %s: %v %s", service, err, resp)
	}
}

func renderWait(w http.ResponseWriter, service string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	waitPage.Execute(w, map[string]string{"Service": service})
}
