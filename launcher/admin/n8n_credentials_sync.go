package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"launcher/config"
)

const n8nLauncherCredPrefix = "Launcher AI · "

func n8nCredentialHTTPClient() *http.Client {
	return &http.Client{Timeout: 90 * time.Second}
}

func n8nAddRequestCookies(req *http.Request, cookies []*http.Cookie) {
	for _, c := range cookies {
		req.AddCookie(c)
	}
}

// n8nPersonalProjectID devuelve el UUID del proyecto personal del usuario n8n.
func n8nPersonalProjectID(u *N8NUser) string {
	if u == nil {
		return ""
	}
	for _, pr := range u.ProjectRelations {
		if pr.Role == "project:personalOwner" {
			return strings.TrimSpace(pr.ID)
		}
	}
	if len(u.ProjectRelations) == 1 {
		return strings.TrimSpace(u.ProjectRelations[0].ID)
	}
	return ""
}

func n8nManagedCredentialName(providerID, email string) string {
	e := strings.TrimSpace(strings.ToLower(email))
	disp := providerID
	for _, p := range RegisteredProviders() {
		if p.ID == providerID {
			disp = p.DisplayName
			break
		}
	}
	return fmt.Sprintf("%s%s · %s", n8nLauncherCredPrefix, disp, e)
}

type n8nCredentialRow struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	HomeProject json.RawMessage `json:"homeProject"`
}

func n8nDecodeCredentialRows(data []byte) ([]n8nCredentialRow, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var arr []n8nCredentialRow
	if data[0] == '[' {
		if err := json.Unmarshal(data, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var wrap struct {
		Data  []n8nCredentialRow `json:"data"`
		Items []n8nCredentialRow `json:"items"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return nil, err
	}
	if len(wrap.Data) > 0 {
		return wrap.Data, nil
	}
	if len(wrap.Items) > 0 {
		return wrap.Items, nil
	}
	return nil, fmt.Errorf("formato de respuesta de credenciales n8n no reconocido")
}

func n8nFilterRowsByProject(rows []n8nCredentialRow, projectID string) []n8nCredentialRow {
	if projectID == "" {
		return rows
	}
	out := make([]n8nCredentialRow, 0, len(rows))
	for _, r := range rows {
		var hp struct {
			ID string `json:"id"`
		}
		if len(r.HomeProject) > 0 && json.Unmarshal(r.HomeProject, &hp) == nil && hp.ID == projectID {
			out = append(out, r)
		}
	}
	return out
}

func n8nFilterRowsForEmail(rows []n8nCredentialRow, email string) []n8nCredentialRow {
	e := strings.TrimSpace(strings.ToLower(email))
	if e == "" {
		return rows
	}
	suf := " · " + e
	out := make([]n8nCredentialRow, 0)
	for _, r := range rows {
		if strings.HasPrefix(r.Name, n8nLauncherCredPrefix) && strings.HasSuffix(strings.ToLower(r.Name), suf) {
			out = append(out, r)
		}
	}
	return out
}

func n8nListCredentials(client *http.Client, cookies []*http.Cookie, projectID string) ([]n8nCredentialRow, error) {
	base := strings.TrimRight(config.N8NInternalURL, "/") + "/rest/credentials"

	do := func(rawURL string) ([]byte, int, error) {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Accept", "application/json")
		if config.N8NBasicUser != "" {
			req.SetBasicAuth(config.N8NBasicUser, config.N8NBasicPass)
		}
		n8nAddRequestCookies(req, cookies)
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		body, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil {
			return nil, resp.StatusCode, rerr
		}
		return body, resp.StatusCode, nil
	}

	if projectID != "" {
		filt, _ := json.Marshal(map[string]string{"projectId": projectID})
		u := base + "?filter=" + url.QueryEscape(string(filt))
		if b, code, err := do(u); err == nil && code == http.StatusOK {
			rows, err := n8nDecodeCredentialRows(b)
			if err == nil && len(rows) > 0 {
				return rows, nil
			}
		}
	}

	b, code, err := do(base)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("n8n list credentials: status %d: %s", code, string(b))
	}
	rows, err := n8nDecodeCredentialRows(b)
	if err != nil {
		return nil, err
	}
	if projectID != "" {
		sub := n8nFilterRowsByProject(rows, projectID)
		if len(sub) > 0 {
			return sub, nil
		}
	}
	return rows, nil
}

func n8nFindManagedCredID(rows []n8nCredentialRow, wantName string) string {
	for _, r := range rows {
		if r.Name == wantName {
			return r.ID
		}
	}
	return ""
}

func n8nPostCredential(client *http.Client, cookies []*http.Cookie, projectID, name, credType string, data map[string]any) error {
	payload := map[string]any{
		"name":      name,
		"type":      credType,
		"data":      data,
		"projectId": projectID,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	u := strings.TrimRight(config.N8NInternalURL, "/") + "/rest/credentials"
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if config.N8NBasicUser != "" {
		req.SetBasicAuth(config.N8NBasicUser, config.N8NBasicPass)
	}
	n8nAddRequestCookies(req, cookies)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func n8nPatchCredential(client *http.Client, cookies []*http.Cookie, credID, name, credType string, data map[string]any) error {
	payload := map[string]any{"name": name, "type": credType, "data": data}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	u := strings.TrimRight(config.N8NInternalURL, "/") + "/rest/credentials/" + url.PathEscape(credID)
	req, err := http.NewRequest(http.MethodPatch, u, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if config.N8NBasicUser != "" {
		req.SetBasicAuth(config.N8NBasicUser, config.N8NBasicPass)
	}
	n8nAddRequestCookies(req, cookies)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func n8nDeleteCredential(client *http.Client, cookies []*http.Cookie, credID string) error {
	u := strings.TrimRight(config.N8NInternalURL, "/") + "/rest/credentials/" + url.PathEscape(credID)
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if config.N8NBasicUser != "" {
		req.SetBasicAuth(config.N8NBasicUser, config.N8NBasicPass)
	}
	n8nAddRequestCookies(req, cookies)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// SyncN8NUserAIKeys crea, actualiza o elimina credenciales IA gestionadas en el proyecto personal del usuario.
func SyncN8NUserAIKeys(n8nUser *N8NUser, companyCanon string) error {
	if config.N8NOwnerEmail == "" || config.N8NOwnerPass == "" || n8nUser == nil {
		return nil
	}
	pid := n8nPersonalProjectID(n8nUser)
	if pid == "" {
		log.Printf("gestión n8n: sin proyecto personal para %s; credenciales IA omitidas hasta que exista el proyecto", n8nUser.Email)
		return nil
	}

	client := n8nCredentialHTTPClient()
	cookies, err := n8nLogin(client)
	if err != nil {
		return fmt.Errorf("n8n login: %w", err)
	}

	rows, err := n8nListCredentials(client, cookies, pid)
	if err != nil {
		return err
	}
	rows = n8nFilterRowsForEmail(rows, n8nUser.Email)

	email := strings.TrimSpace(n8nUser.Email)
	for _, p := range RegisteredProviders() {
		if p.N8NCredentialType == "" {
			continue
		}
		wantName := n8nManagedCredentialName(p.ID, email)
		apiKey, hasKey := CompanyProviderCredentialForSync(companyCanon, p.ID)
		existingID := n8nFindManagedCredID(rows, wantName)

		if !hasKey || strings.TrimSpace(apiKey) == "" {
			if existingID != "" {
				if err := n8nDeleteCredential(client, cookies, existingID); err != nil {
					log.Printf("gestión n8n: no se pudo borrar credencial %q: %v", wantName, err)
				}
			}
			continue
		}

		data := N8NCredentialData(p.ID, strings.TrimSpace(apiKey))
		if existingID != "" {
			if err := n8nPatchCredential(client, cookies, existingID, wantName, p.N8NCredentialType, data); err != nil {
				return fmt.Errorf("actualizar %s: %w", wantName, err)
			}
		} else {
			if err := n8nPostCredential(client, cookies, pid, wantName, p.N8NCredentialType, data); err != nil {
				return fmt.Errorf("crear %s: %w", wantName, err)
			}
		}
	}
	return nil
}

// SyncN8NAllUsersForCompany aplica credenciales de empresa a todos los usuarios n8n asignados a esa empresa (store local).
func SyncN8NAllUsersForCompany(ctx context.Context, companyCanon string) (int, error) {
	_ = ctx
	if config.N8NOwnerEmail == "" {
		return 0, nil
	}
	users, err := getN8NUsers()
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range users {
		u := &users[i]
		co := N8NUserCompany(u.ID, u.Email)
		c2, ok := CanonicalGestionCompany(co)
		if !ok {
			continue
		}
		if c2 != companyCanon {
			continue
		}
		if err := SyncN8NUserAIKeys(u, c2); err != nil {
			log.Printf("gestión n8n: sync %s: %v", u.Email, err)
			continue
		}
		n++
	}
	return n, nil
}

// n8nSyncAIKeysForEmails tras invitaciones: busca usuarios por email y sincroniza credenciales.
func n8nSyncAIKeysForEmails(emails []string) {
	if config.N8NOwnerEmail == "" || len(emails) == 0 {
		return
	}
	users, err := getN8NUsers()
	if err != nil {
		log.Printf("gestión n8n: listar usuarios para sync: %v", err)
		return
	}
	byEmail := make(map[string]*N8NUser, len(users))
	for i := range users {
		e := strings.TrimSpace(strings.ToLower(users[i].Email))
		byEmail[e] = &users[i]
	}
	for _, raw := range emails {
		e := strings.TrimSpace(strings.ToLower(raw))
		u := byEmail[e]
		if u == nil {
			continue
		}
		co := N8NUserCompany(u.ID, u.Email)
		canon, ok := CanonicalGestionCompany(co)
		if !ok {
			canon = strings.TrimSpace(co)
		}
		if canon == "" || !IsValidGestionCompany(canon) {
			continue
		}
		if err := SyncN8NUserAIKeys(u, canon); err != nil {
			log.Printf("gestión n8n: sync tras invitación %s: %v", e, err)
		}
	}
}
