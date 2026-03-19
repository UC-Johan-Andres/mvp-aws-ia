package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"

	"launcher/config"
	"launcher/ui"
)

// HandleLibreChatUsers routes GET/POST to the right function.
func HandleLibreChatUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listLibreChatUsers(w, r)
	case http.MethodPost:
		createLibreChatUsers(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleLibreChatDeleteUser handles DELETE /admin/librechat/users/{email}
func HandleLibreChatDeleteUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	deleteLibreChatUser(w, r)
}

// HandleN8NUsers routes GET/POST
func HandleN8NUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listN8NUsers(w, r)
	case http.MethodPost:
		createN8NUsers(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// jsonOK writes a JSON response with status 200.
func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// jsonError writes a JSON error response.
func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ─────────────────────────────────────────────────────────────────
// Gestion Dashboard
// ─────────────────────────────────────────────────────────────────

// GestionUser represents a user for template rendering.
type GestionUser struct {
	Email string
	Name  string
	Role  string
}

// HandleGestion renders the users management dashboard page shell.
func HandleGestion(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "n8n"
	}
	if tab != "n8n" && tab != "librechat" {
		tab = "n8n"
	}

	ui.RenderGestion(w, tab)
}

// HandleGestionContent renders the inner content (users table + form) for HTMX.
func HandleGestionContent(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "n8n"
	}
	if tab != "n8n" && tab != "librechat" {
		tab = "n8n"
	}

	var users []GestionUser
	var err error

	if tab == "n8n" {
		users, err = fetchN8NUsersStruct()
	} else {
		users, err = fetchLibreChatUsersStruct()
	}

	if err != nil {
		users = []GestionUser{}
	}

	ui.RenderGestionContent(w, tab, users)
}

// HandleGestionSubmit handles POST to create users.
func HandleGestionSubmit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	tab := r.FormValue("tab")
	if tab == "" {
		tab = "n8n"
	}

	var result []byte
	var err error

	if r.FormValue("users_json") != "" {
		result, err = createUsersFromJSON(tab, r.FormValue("users_json"))
	} else if tab == "n8n" {
		result, err = createN8NUsersFromForm(r)
	} else {
		result, err = createLibreChatUsersFromForm(r)
	}

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(result)
}

// createUsersFromJSON creates users from JSON string (CSV import).
func createUsersFromJSON(tab string, usersJSON string) ([]byte, error) {
	var rawUsers []map[string]string
	if err := json.Unmarshal([]byte(usersJSON), &rawUsers); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	if tab == "n8n" {
		requests := make([]n8nUserRequest, 0, len(rawUsers))
		for _, u := range rawUsers {
			email := u["email"]
			role := u["role"]
			if role == "" {
				role = "global:member"
			}
			requests = append(requests, n8nUserRequest{Email: email, Role: role})
		}

		body, _ := json.Marshal(requests)
		client := &http.Client{Timeout: 15 * time.Second}

		cookies, err := n8nLogin(client)
		if err != nil {
			return nil, fmt.Errorf("n8n authentication failed: %w", err)
		}

		req, err := http.NewRequest(http.MethodPost, config.N8NInternalURL+"/rest/invitations", bytes.NewReader(body))
		if err != nil {
			return nil, err
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
			return nil, fmt.Errorf("failed to create n8n users: %w", err)
		}
		defer resp.Body.Close()

		return io.ReadAll(resp.Body)
	}

	type result struct {
		Email   string `json:"email"`
		Created bool   `json:"created"`
		Error   string `json:"error,omitempty"`
	}

	results := make([]result, 0, len(rawUsers))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	coll, client, err := mongoCollection(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer client.Disconnect(ctx)

	for _, u := range rawUsers {
		email := u["email"]
		name := u["name"]
		password := u["password"]
		role := u["role"]

		if email == "" {
			results = append(results, result{Email: email, Created: false, Error: "email is required"})
			continue
		}
		if password == "" {
			results = append(results, result{Email: email, Created: false, Error: "password is required"})
			continue
		}
		if role == "" {
			role = "USER"
		}

		count, err := coll.CountDocuments(ctx, bson.M{"email": email})
		if err != nil {
			results = append(results, result{Email: email, Created: false, Error: "failed to check existing user"})
			continue
		}
		if count > 0 {
			results = append(results, result{Email: email, Created: false, Error: "email already exists"})
			continue
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
		if err != nil {
			results = append(results, result{Email: email, Created: false, Error: "failed to hash password"})
			continue
		}

		username := email
		if idx := strings.Index(email, "@"); idx >= 0 {
			username = email[:idx]
		}

		now := time.Now()
		u := lcUser{
			ID:            primitive.NewObjectID(),
			Name:          name,
			Username:      username,
			Email:         email,
			Password:      string(hash),
			Role:          role,
			Provider:      "local",
			EmailVerified: true,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		_, err = coll.InsertOne(ctx, u)
		if err != nil {
			results = append(results, result{Email: email, Created: false, Error: "failed to insert user"})
			continue
		}

		results = append(results, result{Email: email, Created: true})
	}

	return json.Marshal(results)
}

// fetchN8NUsersStruct fetches n8n users and returns as structured slice.
func fetchN8NUsersStruct() ([]GestionUser, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	cookies, err := n8nLogin(client)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, config.N8NInternalURL+"/rest/users", nil)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("n8n returned status %d: %s", resp.StatusCode, string(data))
	}

	// n8n API returns {"data": ...} where data can be an array or single object
	type n8nUser struct {
		Email string `json:"email"`
		Name  string `json:"firstName"`
		Role  string `json:"role"`
	}

	// Try parsing as {"data": [...]} (array of users)
	var arrayResponse struct {
		Data []n8nUser `json:"data"`
	}
	if err := json.Unmarshal(data, &arrayResponse); err == nil && len(arrayResponse.Data) > 0 {
		users := make([]GestionUser, 0, len(arrayResponse.Data))
		for _, u := range arrayResponse.Data {
			users = append(users, GestionUser{
				Email: u.Email,
				Name:  u.Name,
				Role:  u.Role,
			})
		}
		return users, nil
	}

	// Try parsing as {"data": {...}} (single user object)
	var singleResponse struct {
		Data n8nUser `json:"data"`
	}
	if err := json.Unmarshal(data, &singleResponse); err == nil && singleResponse.Data.Email != "" {
		return []GestionUser{{
			Email: singleResponse.Data.Email,
			Name:  singleResponse.Data.Name,
			Role:  singleResponse.Data.Role,
		}}, nil
	}

	// Fallback: try parsing as direct array (old format)
	var rawUsers []n8nUser
	if err := json.Unmarshal(data, &rawUsers); err == nil {
		users := make([]GestionUser, 0, len(rawUsers))
		for _, u := range rawUsers {
			users = append(users, GestionUser{
				Email: u.Email,
				Name:  u.Name,
				Role:  u.Role,
			})
		}
		return users, nil
	}

	return nil, fmt.Errorf("failed to parse n8n users response")
}

// fetchLibreChatUsersStruct fetches LibreChat users and returns as structured slice.
func fetchLibreChatUsersStruct() ([]GestionUser, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	coll, client, err := mongoCollection(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	cursor, err := coll.Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var lcUsers []lcUser
	if err := cursor.All(ctx, &lcUsers); err != nil {
		return nil, err
	}

	users := make([]GestionUser, 0, len(lcUsers))
	for _, u := range lcUsers {
		users = append(users, GestionUser{
			Email: u.Email,
			Name:  u.Name,
			Role:  u.Role,
		})
	}
	return users, nil
}

// HandleInviteModal renders the n8n invitation modal for a specific email.
func HandleInviteModal(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}

	users, err := fetchN8NUsers()
	if err != nil {
		http.Error(w, "failed to fetch users", http.StatusInternalServerError)
		return
	}

	ui.RenderInviteModal(w, email, string(users))
}

// HandleGestionUsersRows returns the users table rows as HTML fragment for HTMX refresh.
func HandleGestionUsersRows(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "n8n"
	}

	var users []byte
	var err error

	if tab == "n8n" {
		users, err = fetchN8NUsers()
	} else {
		users, err = fetchLibreChatUsers()
	}

	if err != nil {
		users = []byte("[]")
	}

	ui.RenderGestionRows(w, tab, string(users))
}

// Helper: fetch n8n users by calling the internal handler logic
func fetchN8NUsers() ([]byte, error) {
	type n8nUser struct {
		Email string `json:"email"`
		Name  string `json:"firstName"`
		Role  string `json:"role"`
	}

	client := &http.Client{Timeout: 15 * time.Second}

	cookies, err := n8nLogin(client)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, config.N8NInternalURL+"/rest/users", nil)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// Helper: fetch librechat users by calling the internal handler logic
func fetchLibreChatUsers() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	coll, client, err := mongoCollection(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	cursor, err := coll.Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var users []lcUser
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}

	result := make([]lcUserPublic, 0, len(users))
	for _, u := range users {
		result = append(result, lcUserPublic{
			Email:     u.Email,
			Name:      u.Name,
			Role:      u.Role,
			CreatedAt: u.CreatedAt,
		})
	}

	return json.Marshal(result)
}

// Helper: create n8n users from form submission
func createN8NUsersFromForm(r *http.Request) ([]byte, error) {
	emails := r.Form["email"]
	roles := r.Form["role"]

	if len(emails) == 0 {
		return nil, fmt.Errorf("no users to create")
	}

	requests := make([]n8nUserRequest, 0, len(emails))
	for i, email := range emails {
		role := "global:member"
		if i < len(roles) && roles[i] == "global:admin" {
			role = "global:admin"
		}
		requests = append(requests, n8nUserRequest{Email: email, Role: role})
	}

	body, _ := json.Marshal(requests)

	client := &http.Client{Timeout: 15 * time.Second}

	cookies, err := n8nLogin(client)
	if err != nil {
		return nil, fmt.Errorf("n8n authentication failed: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, config.N8NInternalURL+"/rest/invitations", bytes.NewReader(body))
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("failed to create n8n users: %w", err)
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// Helper: create librechat users from form submission
func createLibreChatUsersFromForm(r *http.Request) ([]byte, error) {
	emails := r.Form["email"]
	names := r.Form["name"]
	passwords := r.Form["password"]
	roles := r.Form["role"]

	if len(emails) == 0 {
		return nil, fmt.Errorf("no users to create")
	}

	type createReq struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}

	requests := make([]createReq, 0, len(emails))
	for i, email := range emails {
		name := email
		if i < len(names) && names[i] != "" {
			name = names[i]
		}
		password := ""
		if i < len(passwords) {
			password = passwords[i]
		}
		role := "USER"
		if i < len(roles) && roles[i] == "ADMIN" {
			role = "ADMIN"
		}
		requests = append(requests, createReq{Email: email, Name: name, Password: password, Role: role})
	}

	coll, client, err := mongoCollection(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer client.Disconnect(context.Background())

	type result struct {
		Email   string `json:"email"`
		Created bool   `json:"created"`
		Error   string `json:"error,omitempty"`
	}

	results := make([]result, 0, len(requests))

	for _, req := range requests {
		if req.Email == "" {
			results = append(results, result{Email: req.Email, Created: false, Error: "email is required"})
			continue
		}
		if req.Password == "" {
			results = append(results, result{Email: req.Email, Created: false, Error: "password is required"})
			continue
		}

		role := req.Role
		if role == "" {
			role = "USER"
		}
		if role != "USER" && role != "ADMIN" {
			results = append(results, result{Email: req.Email, Created: false, Error: "invalid role"})
			continue
		}

		count, err := coll.CountDocuments(context.Background(), bson.M{"email": req.Email})
		if err != nil {
			results = append(results, result{Email: req.Email, Created: false, Error: "failed to check existing user"})
			continue
		}
		if count > 0 {
			results = append(results, result{Email: req.Email, Created: false, Error: "email already exists"})
			continue
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
		if err != nil {
			results = append(results, result{Email: req.Email, Created: false, Error: "failed to hash password"})
			continue
		}

		username := req.Email
		if idx := strings.Index(req.Email, "@"); idx >= 0 {
			username = req.Email[:idx]
		}

		now := time.Now()
		u := lcUser{
			ID:            primitive.NewObjectID(),
			Name:          req.Name,
			Username:      username,
			Email:         req.Email,
			Password:      string(hash),
			Role:          role,
			Provider:      "local",
			EmailVerified: true,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		_, err = coll.InsertOne(context.Background(), u)
		if err != nil {
			results = append(results, result{Email: req.Email, Created: false, Error: "failed to insert user"})
			continue
		}

		results = append(results, result{Email: req.Email, Created: true})
	}

	return json.Marshal(results)
}
