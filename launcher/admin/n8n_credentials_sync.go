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
	return nil, nil
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
			if err == nil {
				log.Printf("n8n-cred-sync: listCredentials con filtro projectId=%q → %d filas", projectID, len(rows))
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
	log.Printf("n8n-cred-sync: listCredentials sin filtro → %d filas totales", len(rows))
	if projectID != "" {
		sub := n8nFilterRowsByProject(rows, projectID)
		log.Printf("n8n-cred-sync: tras filtrar por projectId=%q → %d filas", projectID, len(sub))
		return sub, nil
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
		"name": name,
		"type": credType,
		"data": data,
	}
	if projectID != "" {
		payload["projectId"] = projectID
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	log.Printf("n8n-cred-sync POST /rest/credentials name=%q type=%q projectId=%q", name, credType, projectID)
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
		return fmt.Errorf("POST /rest/credentials: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		log.Printf("n8n-cred-sync POST FAILED status=%d body=%s", resp.StatusCode, string(body))
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	preview := string(body)
	if len(preview) > 200 {
		preview = preview[:200]
	}
	log.Printf("n8n-cred-sync POST OK name=%q resp=%s", name, preview)
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

func n8nErrLooksLikeRateLimit(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "429") || strings.Contains(s, "too many requests")
}

// syncN8NUserAIKeysWithSession usa cookies ya obtenidas (un solo login por lote de usuarios → evita 429 en n8n).
func syncN8NUserAIKeysWithSession(client *http.Client, cookies []*http.Cookie, n8nUser *N8NUser, companyCanon string) error {
	if n8nUser == nil {
		return nil
	}
	log.Printf("n8n-cred-sync: inicio para email=%s company=%q projectRelations=%d",
		n8nUser.Email, companyCanon, len(n8nUser.ProjectRelations))
	for i, pr := range n8nUser.ProjectRelations {
		log.Printf("n8n-cred-sync:   projectRelation[%d] id=%q role=%q name=%q", i, pr.ID, pr.Role, pr.Name)
	}

	pid := n8nPersonalProjectID(n8nUser)
	if pid == "" {
		log.Printf("n8n-cred-sync: SIN proyecto personal para %s (projectRelations=%d)", n8nUser.Email, len(n8nUser.ProjectRelations))
		return fmt.Errorf("sin proyecto personal n8n para %s (¿invitación sin aceptar? o sin N8N_POSTGRES_DSN para leer project_relation)", strings.TrimSpace(n8nUser.Email))
	}
	log.Printf("n8n-cred-sync: proyecto personal id=%q para %s", pid, n8nUser.Email)

	rows, err := n8nListCredentials(client, cookies, pid)
	if err != nil {
		log.Printf("n8n-cred-sync: error listando credenciales pid=%q: %v", pid, err)
		return err
	}
	log.Printf("n8n-cred-sync: %d credenciales encontradas en proyecto %q", len(rows), pid)
	for _, r := range rows {
		log.Printf("n8n-cred-sync:   cred id=%q name=%q type=%q", r.ID, r.Name, r.Type)
	}

	managedRows := n8nFilterRowsForEmail(rows, n8nUser.Email)
	log.Printf("n8n-cred-sync: %d credenciales gestionadas (Launcher AI · …) para %s", len(managedRows), n8nUser.Email)

	email := strings.TrimSpace(n8nUser.Email)
	for _, p := range RegisteredProviders() {
		if p.N8NCredentialType == "" {
			continue
		}
		wantName := n8nManagedCredentialName(p.ID, email)
		apiKey, hasKey := CompanyProviderCredentialForSync(companyCanon, p.ID)
		existingID := n8nFindManagedCredID(managedRows, wantName)
		log.Printf("n8n-cred-sync: proveedor=%s wantName=%q hasKey=%v existingID=%q", p.ID, wantName, hasKey, existingID)

		if !hasKey || strings.TrimSpace(apiKey) == "" {
			if existingID != "" {
				log.Printf("n8n-cred-sync: borrando credencial existente %q", existingID)
				if err := n8nDeleteCredential(client, cookies, existingID); err != nil {
					log.Printf("n8n-cred-sync: error borrando %q: %v", wantName, err)
				}
			} else {
				log.Printf("n8n-cred-sync: sin key para %s y sin credencial existente → nada que hacer", p.ID)
			}
			continue
		}

		data := N8NCredentialData(p.ID, strings.TrimSpace(apiKey))
		if existingID != "" {
			log.Printf("n8n-cred-sync: PATCH credencial existente id=%q name=%q", existingID, wantName)
			if err := n8nPatchCredential(client, cookies, existingID, wantName, p.N8NCredentialType, data); err != nil {
				return fmt.Errorf("actualizar %s: %w", wantName, err)
			}
		} else {
			log.Printf("n8n-cred-sync: POST nueva credencial name=%q type=%q en proyecto=%q", wantName, p.N8NCredentialType, pid)
			if err := n8nPostCredential(client, cookies, pid, wantName, p.N8NCredentialType, data); err != nil {
				return fmt.Errorf("crear %s: %w", wantName, err)
			}
		}
	}
	log.Printf("n8n-cred-sync: FIN para %s", n8nUser.Email)
	return nil
}

// SyncN8NUserAIKeys crea, actualiza o elimina credenciales IA gestionadas en el proyecto personal del usuario.
func SyncN8NUserAIKeys(n8nUser *N8NUser, companyCanon string) error {
	if config.N8NOwnerEmail == "" || config.N8NOwnerPass == "" || n8nUser == nil {
		log.Printf("n8n-cred-sync: skip (owner creds vacías o user nil)")
		return nil
	}
	client := n8nCredentialHTTPClient()
	cookies, err := n8nLogin(client)
	if err != nil {
		return fmt.Errorf("n8n login: %w", err)
	}
	return syncN8NUserAIKeysWithSession(client, cookies, n8nUser, companyCanon)
}

// SyncN8NAllUsersForCompany aplica credenciales de empresa a todos los usuarios n8n asignados a esa empresa (store local).
func SyncN8NAllUsersForCompany(ctx context.Context, companyCanon string) (int, error) {
	_ = ctx
	if config.N8NOwnerEmail == "" {
		log.Printf("n8n-cred-sync: N8N_OWNER_EMAIL vacío, omitiendo sync")
		return 0, nil
	}
	log.Printf("n8n-cred-sync: SyncN8NAllUsersForCompany empresa=%q", companyCanon)
	users, err := getN8NUsers()
	if err != nil {
		log.Printf("n8n-cred-sync: error obteniendo usuarios n8n: %v", err)
		return 0, err
	}
	log.Printf("n8n-cred-sync: %d usuarios n8n obtenidos", len(users))

	var targets []*N8NUser
	for i := range users {
		u := &users[i]
		co := N8NUserCompany(u.ID, u.Email)
		c2, ok := CanonicalGestionCompany(co)
		log.Printf("n8n-cred-sync: usuario %s (id=%s) company-store=%q canonical=%q ok=%v",
			u.Email, u.ID, co, c2, ok)
		if !ok {
			log.Printf("n8n-cred-sync: skip %s — empresa %q no es canónica", u.Email, co)
			continue
		}
		if c2 != companyCanon {
			log.Printf("n8n-cred-sync: skip %s — empresa %q ≠ %q", u.Email, c2, companyCanon)
			continue
		}
		targets = append(targets, u)
	}

	if len(targets) == 0 {
		log.Printf("n8n-cred-sync: SyncN8NAllUsersForCompany empresa=%q → 0 usuario(s) procesados", companyCanon)
		return 0, nil
	}

	client := n8nCredentialHTTPClient()
	cookies, err := n8nLogin(client)
	if err != nil {
		return 0, fmt.Errorf("n8n login: %w", err)
	}
	log.Printf("n8n-cred-sync: sesión n8n única para %d usuario(s) de empresa %q", len(targets), companyCanon)

	n := 0
	for _, u := range targets {
		log.Printf("n8n-cred-sync: MATCH %s → empresa %q, sincronizando…", u.Email, companyCanon)
		err := syncN8NUserAIKeysWithSession(client, cookies, u, companyCanon)
		if err != nil && n8nErrLooksLikeRateLimit(err) {
			log.Printf("n8n-cred-sync: 429 en sync %s, re-login tras pausa…", u.Email)
			time.Sleep(3 * time.Second)
			var loginErr error
			cookies, loginErr = n8nLogin(client)
			if loginErr != nil {
				log.Printf("n8n-cred-sync: ERROR re-login: %v", loginErr)
				continue
			}
			err = syncN8NUserAIKeysWithSession(client, cookies, u, companyCanon)
		}
		if err != nil {
			log.Printf("n8n-cred-sync: ERROR sync %s: %v", u.Email, err)
			continue
		}
		n++
	}
	log.Printf("n8n-cred-sync: SyncN8NAllUsersForCompany empresa=%q → %d usuario(s) procesados", companyCanon, n)
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

	type pair struct {
		u     *N8NUser
		canon string
	}
	var batch []pair
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
		batch = append(batch, pair{u: u, canon: canon})
	}
	if len(batch) == 0 {
		return
	}

	client := n8nCredentialHTTPClient()
	cookies, err := n8nLogin(client)
	if err != nil {
		log.Printf("gestión n8n: sync tras invitación login: %v", err)
		return
	}
	for _, it := range batch {
		err := syncN8NUserAIKeysWithSession(client, cookies, it.u, it.canon)
		if err != nil && n8nErrLooksLikeRateLimit(err) {
			time.Sleep(3 * time.Second)
			cookies, err = n8nLogin(client)
			if err != nil {
				log.Printf("gestión n8n: sync tras invitación re-login: %v", err)
				continue
			}
			err = syncN8NUserAIKeysWithSession(client, cookies, it.u, it.canon)
		}
		if err != nil {
			log.Printf("gestión n8n: sync tras invitación %s: %v", strings.ToLower(strings.TrimSpace(it.u.Email)), err)
		}
	}
}
