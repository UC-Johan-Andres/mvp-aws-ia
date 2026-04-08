package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"launcher/config"
	"launcher/email"
)

func n8nHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

var (
	n8nOwnerSessMu      sync.Mutex
	n8nOwnerSessCookies []*http.Cookie
	n8nOwnerSessUntil   time.Time
)

const n8nOwnerSessionTTL = 5 * time.Minute

func cloneHTTPCookies(src []*http.Cookie) []*http.Cookie {
	if len(src) == 0 {
		return nil
	}
	out := make([]*http.Cookie, 0, len(src))
	for _, c := range src {
		if c == nil {
			continue
		}
		cp := *c
		out = append(out, &cp)
	}
	return out
}

// n8nInvalidateOwnerSession borra la caché de cookies (p. ej. 401 o tras 429 antes de re-login).
func n8nInvalidateOwnerSession() {
	n8nOwnerSessMu.Lock()
	defer n8nOwnerSessMu.Unlock()
	n8nOwnerSessCookies = nil
	n8nOwnerSessUntil = time.Time{}
}

// n8nOwnerCookies devuelve cookies de sesión del owner reutilizando caché breve para no disparar 429 en n8n.
func n8nOwnerCookies(client *http.Client) ([]*http.Cookie, error) {
	n8nOwnerSessMu.Lock()
	if len(n8nOwnerSessCookies) > 0 && time.Now().Before(n8nOwnerSessUntil) {
		out := cloneHTTPCookies(n8nOwnerSessCookies)
		n8nOwnerSessMu.Unlock()
		return out, nil
	}
	n8nOwnerSessMu.Unlock()

	cookies, err := n8nLogin(client)
	if err != nil {
		return nil, err
	}
	n8nOwnerSessMu.Lock()
	n8nOwnerSessCookies = cloneHTTPCookies(cookies)
	n8nOwnerSessUntil = time.Now().Add(n8nOwnerSessionTTL)
	n8nOwnerSessMu.Unlock()
	return cloneHTTPCookies(cookies), nil
}

// n8nLoginOnce un solo POST /rest/login.
func n8nLoginOnce(client *http.Client) ([]*http.Cookie, error) {
	body, _ := json.Marshal(map[string]string{
		"emailOrLdapLoginId": config.N8NOwnerEmail,
		"password":           config.N8NOwnerPass,
	})

	req, err := http.NewRequest(http.MethodPost, config.N8NInternalURL+"/rest/login", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if config.N8NBasicUser != "" {
		req.SetBasicAuth(config.N8NBasicUser, config.N8NBasicPass)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("n8n login request failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("n8n login failed with status %d: %s", resp.StatusCode, string(data))
	}

	return resp.Cookies(), nil
}

// n8nLogin autentica con reintentos si n8n responde 429 (rate limit).
func n8nLogin(client *http.Client) ([]*http.Cookie, error) {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			d := time.Duration(1+attempt*2) * time.Second
			if d > 10*time.Second {
				d = 10 * time.Second
			}
			log.Printf("n8n: esperando %v antes de reintentar login (intento %d/5)", d, attempt+1)
			time.Sleep(d)
		}
		cookies, err := n8nLoginOnce(client)
		if err == nil {
			return cookies, nil
		}
		lastErr = err
		s := strings.ToLower(err.Error())
		if !strings.Contains(s, "429") && !strings.Contains(s, "too many requests") {
			return nil, err
		}
	}
	return nil, lastErr
}

func listN8NUsers(w http.ResponseWriter, r *http.Request) {
	if config.N8NOwnerEmail == "" || config.N8NOwnerPass == "" {
		jsonError(w, "n8n owner credentials not configured", http.StatusServiceUnavailable)
		return
	}

	users, err := getN8NUsers()
	if err != nil {
		jsonError(w, "failed to list n8n users: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"count": len(users),
			"items": users,
		},
	})
}

type n8nUserRequest struct {
	Email   string `json:"email"`
	Role    string `json:"role"`
	Company string `json:"company,omitempty"` // no se envía a n8n; solo store local
}

type n8nInviteAPIItem struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type n8nEmailDeliveryResult struct {
	Email     string `json:"email"`
	Sent      bool   `json:"sent"`
	Queued    bool   `json:"queued"`
	Error     string `json:"error,omitempty"`
	UserID    string `json:"userId,omitempty"`
	ResetLink string `json:"resetLink,omitempty"`
}

func createN8NUsers(w http.ResponseWriter, r *http.Request) {
	if config.N8NOwnerEmail == "" || config.N8NOwnerPass == "" {
		jsonError(w, "n8n owner credentials not configured", http.StatusServiceUnavailable)
		return
	}

	var requests []n8nUserRequest
	if err := json.NewDecoder(r.Body).Decode(&requests); err != nil {
		jsonError(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(requests) == 0 {
		jsonError(w, "empty user list", http.StatusBadRequest)
		return
	}

	for _, req := range requests {
		if req.Role != "global:owner" && req.Role != "global:member" {
			jsonError(w, fmt.Sprintf("invalid role %q: must be global:owner or global:member", req.Role), http.StatusBadRequest)
			return
		}
	}
	for _, req := range requests {
		c := strings.TrimSpace(req.Company)
		if c != "" && !IsValidGestionCompany(c) {
			jsonError(w, fmt.Sprintf("empresa no válida: %q", req.Company), http.StatusBadRequest)
			return
		}
	}

	client := n8nHTTPClient()

	cookies, err := n8nOwnerCookies(client)
	if err != nil {
		jsonError(w, "n8n authentication failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	apiPayload := make([]n8nInviteAPIItem, len(requests))
	for i, req := range requests {
		apiPayload[i] = n8nInviteAPIItem{Email: req.Email, Role: req.Role}
	}
	body, _ := json.Marshal(apiPayload)

	req, err := http.NewRequest(http.MethodPost, config.N8NInternalURL+"/rest/invitations", bytes.NewReader(body))
	if err != nil {
		jsonError(w, "failed to create request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if config.N8NBasicUser != "" {
		req.SetBasicAuth(config.N8NBasicUser, config.N8NBasicPass)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		jsonError(w, "failed to create n8n users: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		jsonError(w, "failed to read n8n response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		jsonError(w, fmt.Sprintf("n8n returned status %d: %s", resp.StatusCode, string(data)), http.StatusBadGateway)
		return
	}

	rows := make([]N8NEmailCompanyRow, 0, len(requests))
	for _, req := range requests {
		rows = append(rows, N8NEmailCompanyRow{Email: req.Email, Company: req.Company})
	}
	if err := PersistN8NEmailCompanies(rows); err != nil {
		log.Printf("gestion: persistir empresa n8n (API): %v", err)
	}

	emails := make([]string, 0, len(requests))
	for _, req := range requests {
		emails = append(emails, req.Email)
	}
	n8nSyncAIKeysForEmails(emails)

	results := make([]n8nEmailDeliveryResult, 0, len(requests))
	users, listErr := getN8NUsers()
	usersByEmail := map[string]N8NUser{}
	if listErr == nil {
		for _, u := range users {
			usersByEmail[strings.ToLower(strings.TrimSpace(u.Email))] = u
		}
	}

	for _, req := range requests {
		r := n8nEmailDeliveryResult{Email: req.Email}
		u, ok := usersByEmail[strings.ToLower(strings.TrimSpace(req.Email))]
		if !ok {
			r.Error = "usuario creado en n8n sin ID disponible para generar enlace"
			results = append(results, r)
			continue
		}
		r.UserID = u.ID
		link, err := fetchN8NPasswordResetLink(u.ID)
		if err != nil {
			r.Error = err.Error()
			results = append(results, r)
			continue
		}
		r.ResetLink = link

		resetBody := fmt.Sprintf(
			`<h2>Activa tu cuenta de n8n</h2>
			<p>Hola,</p>
			<p>Haz clic en el siguiente enlace para definir tu contraseña e iniciar sesión:</p>
			<p><a href="%s">%s</a></p>
			<p>Si no solicitaste este acceso, ignora este correo.</p>`,
			link, link,
		)
		queued, sendErr := email.SendEmailWhenVerified(req.Email, "Acceso a n8n: configura tu contraseña", resetBody)
		if sendErr != nil {
			r.Error = sendErr.Error()
		} else {
			r.Queued = queued
			r.Sent = !queued
		}
		results = append(results, r)
	}

	jsonOK(w, map[string]interface{}{
		"created":      len(requests),
		"n8n_response": json.RawMessage(data),
		"deliveries":   results,
	})
}

func deleteN8NUser(w http.ResponseWriter, r *http.Request) {
	if config.N8NOwnerEmail == "" || config.N8NOwnerPass == "" {
		jsonError(w, "n8n owner credentials not configured", http.StatusServiceUnavailable)
		return
	}

	type deleteRequest struct {
		ID string `json:"id"`
	}
	var reqBody deleteRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		jsonError(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if reqBody.ID == "" {
		jsonError(w, "user id is required", http.StatusBadRequest)
		return
	}

	client := n8nHTTPClient()

	cookies, err := n8nOwnerCookies(client)
	if err != nil {
		jsonError(w, "n8n authentication failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	req, err := http.NewRequest(http.MethodDelete, config.N8NInternalURL+"/rest/users/"+reqBody.ID, nil)
	if err != nil {
		jsonError(w, "failed to create request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if config.N8NBasicUser != "" {
		req.SetBasicAuth(config.N8NBasicUser, config.N8NBasicPass)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		jsonError(w, "failed to delete n8n user: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		jsonError(w, "failed to read n8n response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		jsonError(w, fmt.Sprintf("n8n returned status %d: %s", resp.StatusCode, string(data)), http.StatusBadGateway)
		return
	}

	jsonOK(w, map[string]string{"deleted": reqBody.ID})
}

// HandleN8NPasswordResetLink POST {"id":"uuid"} → enlace de restablecimiento (API n8n).
func HandleN8NPasswordResetLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if config.N8NOwnerEmail == "" || config.N8NOwnerPass == "" {
		jsonError(w, "n8n owner credentials not configured", http.StatusServiceUnavailable)
		return
	}
	var reqBody struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		jsonError(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(reqBody.ID) == "" {
		jsonError(w, "user id is required", http.StatusBadRequest)
		return
	}
	link, err := fetchN8NPasswordResetLink(strings.TrimSpace(reqBody.ID))
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Enviar email con el enlace de reset (best effort)
	emailSent := false
	emailQueued := false
	emailError := ""
	if reqBody.Email != "" {
		resetBody := fmt.Sprintf(
			`<h2>Restablece tu contraseña</h2>
			<p>Hola,</p>
			<p>Haz clic en el siguiente enlace para restablecer tu contraseña:</p>
			<p><a href="%s">%s</a></p>
			<p>Si no solicitaste esto, ignora este email.</p>`,
			link, link,
		)
		queued, err := email.SendEmailWhenVerified(reqBody.Email, "Restablecer contraseña", resetBody)
		if err != nil {
			log.Printf("gestion: error enviando password reset a %s: %v", reqBody.Email, err)
			emailError = err.Error()
		} else {
			emailQueued = queued
			emailSent = !queued
		}
	}

	jsonOK(w, map[string]interface{}{"link": link, "sent": emailSent, "queued": emailQueued, "error": emailError})
}

func fetchN8NPasswordResetLink(userID string) (string, error) {
	client := n8nHTTPClient()
	cookies, err := n8nOwnerCookies(client)
	if err != nil {
		return "", fmt.Errorf("n8n authentication failed: %w", err)
	}
	u := strings.TrimRight(config.N8NInternalURL, "/") + "/rest/users/" + url.PathEscape(userID) + "/password-reset-link"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if config.N8NBasicUser != "" {
		req.SetBasicAuth(config.N8NBasicUser, config.N8NBasicPass)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("n8n request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("n8n returned status %d: %s", resp.StatusCode, string(data))
	}
	var out struct {
		Link string `json:"link"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("invalid n8n response: %w", err)
	}
	if strings.TrimSpace(out.Link) == "" {
		return "", fmt.Errorf("n8n no devolvió enlace (¿versión antigua sin /password-reset-link?)")
	}
	return out.Link, nil
}

type n8nUserUpdateRequest struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Role      string `json:"role"`
}

func updateN8NUser(w http.ResponseWriter, r *http.Request) {
	if config.N8NOwnerEmail == "" || config.N8NOwnerPass == "" {
		jsonError(w, "n8n owner credentials not configured", http.StatusServiceUnavailable)
		return
	}

	var reqBody n8nUserUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		jsonError(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if reqBody.ID == "" {
		jsonError(w, "user id is required", http.StatusBadRequest)
		return
	}

	if reqBody.Role != "global:owner" && reqBody.Role != "global:member" && reqBody.Role != "global:admin" {
		jsonError(w, "invalid role: must be global:owner, global:member or global:admin", http.StatusBadRequest)
		return
	}

	client := n8nHTTPClient()

	cookies, err := n8nOwnerCookies(client)
	if err != nil {
		jsonError(w, "n8n authentication failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPut, config.N8NInternalURL+"/rest/users/"+reqBody.ID, bytes.NewReader(body))
	if err != nil {
		jsonError(w, "failed to create request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if config.N8NBasicUser != "" {
		req.SetBasicAuth(config.N8NBasicUser, config.N8NBasicPass)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		jsonError(w, "failed to update n8n user: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		jsonError(w, "failed to read n8n response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		jsonError(w, fmt.Sprintf("n8n returned status %d: %s", resp.StatusCode, string(data)), http.StatusBadGateway)
		return
	}

	jsonOK(w, map[string]string{"updated": reqBody.ID})
}
