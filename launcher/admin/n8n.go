package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"launcher/config"
)

func n8nHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// n8nLogin authenticates with n8n using owner credentials and basic auth,
// returning the session cookies from the response.
func n8nLogin(client *http.Client) ([]*http.Cookie, error) {
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

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("n8n login failed with status %d: %s", resp.StatusCode, string(data))
	}

	return resp.Cookies(), nil
}

func listN8NUsers(w http.ResponseWriter, r *http.Request) {
	if config.N8NOwnerEmail == "" || config.N8NOwnerPass == "" {
		jsonError(w, "n8n owner credentials not configured", http.StatusServiceUnavailable)
		return
	}

	client := n8nHTTPClient()

	cookies, err := n8nLogin(client)
	if err != nil {
		jsonError(w, "n8n authentication failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	req, err := http.NewRequest(http.MethodGet, config.N8NInternalURL+"/api/v1/users", nil)
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
		jsonError(w, "failed to list n8n users: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		jsonError(w, "failed to read n8n response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK {
		jsonError(w, fmt.Sprintf("n8n returned status %d: %s", resp.StatusCode, string(data)), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

type n8nUserRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
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
		if req.Role != "global:admin" && req.Role != "global:member" {
			jsonError(w, fmt.Sprintf("invalid role %q: must be global:admin or global:member", req.Role), http.StatusBadRequest)
			return
		}
	}

	client := n8nHTTPClient()

	cookies, err := n8nLogin(client)
	if err != nil {
		jsonError(w, "n8n authentication failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	body, _ := json.Marshal(requests)

	req, err := http.NewRequest(http.MethodPost, config.N8NInternalURL+"/api/v1/users", bytes.NewReader(body))
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

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
