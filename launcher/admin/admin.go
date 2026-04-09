package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"

	"launcher/config"
	emailpkg "launcher/email"
	"launcher/ui"
)

// HandleLibreChatUsers routes GET/POST/DELETE/PUT to the right function.
func HandleLibreChatUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listLibreChatUsers(w, r)
	case http.MethodPost:
		createLibreChatUsers(w, r)
	case http.MethodDelete:
		deleteLibreChatUser(w, r)
	case http.MethodPut:
		updateLibreChatUser(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleVerificationRetry handles POST /admin/verification/retry
func HandleVerificationRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Email == "" {
		jsonError(w, "email is required", http.StatusBadRequest)
		return
	}

	if err := RetryVerification(req.Email); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"retry": "started", "email": req.Email})
}

// HandleLibreChatDeleteUser handles DELETE /admin/librechat/users/{email}
func HandleLibreChatDeleteUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	deleteLibreChatUser(w, r)
}

// HandleN8NDeleteUser handles DELETE /admin/n8n/users/{id}
func HandleN8NDeleteUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	deleteN8NUser(w, r)
}

// HandleLibreChatUpdateUser handles PUT /admin/librechat/users/{email}
func HandleLibreChatUpdateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	updateLibreChatUser(w, r)
}

// HandleN8NUpdateUser handles PUT /admin/n8n/users/{id}
func HandleN8NUpdateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	updateN8NUser(w, r)
}

// HandleN8NUsers routes GET/POST/DELETE/PUT
func HandleN8NUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listN8NUsers(w, r)
	case http.MethodPost:
		createN8NUsers(w, r)
	case http.MethodDelete:
		deleteN8NUser(w, r)
	case http.MethodPut:
		updateN8NUser(w, r)
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

// getN8NUsers fetches users from n8n API and returns parsed data.
func getN8NUsers() ([]N8NUser, error) {
	if config.N8NOwnerEmail == "" || config.N8NOwnerPass == "" {
		return nil, fmt.Errorf("n8n owner credentials not configured")
	}

	client := n8nHTTPClient()

	var body []byte
	for attempt := 0; attempt < 2; attempt++ {
		cookies, err := n8nOwnerCookies(client)
		if err != nil {
			return nil, fmt.Errorf("n8n authentication failed: %w", err)
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
		b, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil {
			return nil, fmt.Errorf("n8n read body: %w", rerr)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			n8nInvalidateOwnerSession()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("n8n returned status %d: %s", resp.StatusCode, string(b))
		}
		body = b
		break
	}

	var items []N8NUser

	var paginated struct {
		Data struct {
			Count int       `json:"count"`
			Items []N8NUser `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &paginated); err == nil && len(paginated.Data.Items) > 0 {
		items = paginated.Data.Items
	}

	if len(items) == 0 {
		var direct struct {
			Count int       `json:"count"`
			Items []N8NUser `json:"items"`
		}
		if err := json.Unmarshal(body, &direct); err == nil && len(direct.Items) > 0 {
			items = direct.Items
		}
	}

	if len(items) == 0 {
		var arr struct {
			Data []N8NUser `json:"data"`
		}
		if err := json.Unmarshal(body, &arr); err == nil && len(arr.Data) > 0 {
			items = arr.Data
		}
	}

	if len(items) == 0 {
		preview := string(body)
		if len(preview) > 500 {
			preview = preview[:500]
		}
		log.Printf("n8n-users: no se pudieron parsear usuarios; raw=%s", preview)
		return nil, fmt.Errorf("no se pudieron parsear usuarios de n8n")
	}

	enrichN8NUsersFromPostgres(items)

	for i := range items {
		log.Printf("n8n-users: [%d] id=%s email=%s projectRelations=%d",
			i, items[i].ID, items[i].Email, len(items[i].ProjectRelations))
	}
	ReconcileN8NCompanyIDs(items)
	for i := range items {
		items[i].Company = N8NUserCompany(items[i].ID, items[i].Email)
	}

	return items, nil
}

// getLibreChatUsers fetches users from LibreChat MongoDB.
func getLibreChatUsers() ([]LibreChatUser, error) {
	if config.MongoURI == "" {
		return nil, fmt.Errorf("MongoDB not configured")
	}

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

	defCo := GestionDefaultCompany()
	result := make([]LibreChatUser, 0, len(users))
	for _, u := range users {
		co := strings.TrimSpace(u.Company)
		if co == "" {
			co = defCo
		}
		verifStatus := getVerificationStatusForEmail(u.Email)
		if verifStatus == "" {
			if u.EmailVerified {
				verifStatus = "verified"
			} else {
				verifStatus = "pending"
			}
		}
		canRetry, remainingAttempts := getVerificationRetryInfo(u.Email)
		result = append(result, LibreChatUser{
			ID:                 u.ID.Hex(),
			Email:              u.Email,
			Name:               u.Name,
			Role:               u.Role,
			Company:            co,
			CreatedAt:          u.CreatedAt.Format(time.RFC3339),
			VerificationStatus: verifStatus,
			CanRetry:           canRetry,
			RemainingAttempts:  remainingAttempts,
		})
	}

	return result, nil
}

// ─────────────────────────────────────────────────────────────────
// Gestion Dashboard
// ─────────────────────────────────────────────────────────────────

// GestionUser represents a user for template rendering.
type GestionUser struct {
	ID                 string
	Email              string
	Name               string
	FirstName          string
	LastName           string
	Role               string
	Company            string
	InviteURL          string
	IsPending          bool
	CreatedAt          string
	VerificationStatus string
	CanRetry           bool
	RemainingAttempts  int
	// n8n: estadísticas desde PostgreSQL (misma consulta que en psql)
	WorkflowsAccesibles   int64
	TotalExecutions       int64
	ProdExecutions        int64
	FailedTotalExecutions int64
	FailedProdExecutions  int64
	FailureRatePct        float64
	RunTimeAvgSeconds     float64
}

// N8NProjectRelation is a user's membership in an n8n project (RBAC).
type N8NProjectRelation struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	Name string `json:"name"`
}

// N8NUser represents a user from n8n API.
type N8NUser struct {
	ID               string               `json:"id"`
	Email            string               `json:"email"`
	FirstName        string               `json:"firstName"`
	LastName         string               `json:"lastName"`
	Role             string               `json:"role"`
	IsPending        bool                 `json:"isPending"`
	InviteAcceptURL  string               `json:"inviteAcceptUrl"`
	ProjectRelations []N8NProjectRelation `json:"projectRelations"`
	Company          string               `json:"company,omitempty"`

	WorkflowsAccesibles   int64   `json:"workflowsAccesibles"`
	TotalExecutions       int64   `json:"totalExecutions"`
	ProdExecutions        int64   `json:"prodExecutions"`
	FailedTotalExecutions int64   `json:"failedTotalExecutions"`
	FailedProdExecutions  int64   `json:"failedProdExecutions"`
	FailureRatePct        float64 `json:"failureRatePct"`
	RunTimeAvgSeconds     float64 `json:"runTimeAvgSeconds"`

	VerificationStatus string `json:"verificationStatus"`
}

// LibreChatUser represents a user from LibreChat.
type LibreChatUser struct {
	ID                 string `json:"id"`
	Email              string `json:"email"`
	Name               string `json:"name"`
	Role               string `json:"role"`
	Company            string `json:"company,omitempty"`
	CreatedAt          string `json:"createdAt"`
	VerificationStatus string `json:"verificationStatus"`
	CanRetry           bool   `json:"canRetry"`
	RemainingAttempts  int    `json:"remainingAttempts"`
}

// gestionTab reads tab from query (GET /gestion/content?tab=) or form field (POST /gestion).
func gestionTab(r *http.Request) string {
	tab := r.URL.Query().Get("tab")
	if tab != "" {
		switch tab {
		case "n8n", "librechat", "empresas":
			return tab
		default:
			return "n8n"
		}
	}
	_ = r.ParseForm()
	tab = r.FormValue("tab")
	switch tab {
	case "n8n", "librechat":
		return tab
	default:
		return "n8n"
	}
}

// gestionUsersForTab loads users for the gestión table.
func gestionUsersForTab(tab string) ([]GestionUser, error) {
	var users []GestionUser
	if tab == "n8n" {
		n8nUsers, err := getN8NUsers()
		if err != nil {
			return nil, err
		}
		for _, u := range n8nUsers {
			name := u.FirstName
			if u.LastName != "" {
				name += " " + u.LastName
			}
			if name == "" {
				name = "-"
			}
			n8nStatus := computeN8NVerificationStatus(u)
			users = append(users, GestionUser{
				ID:                    u.ID,
				Email:                 u.Email,
				Name:                  name,
				FirstName:             u.FirstName,
				LastName:              u.LastName,
				Role:                  u.Role,
				Company:               u.Company,
				InviteURL:             u.InviteAcceptURL,
				IsPending:             u.IsPending,
				VerificationStatus:    n8nStatus,
				WorkflowsAccesibles:   u.WorkflowsAccesibles,
				TotalExecutions:       u.TotalExecutions,
				ProdExecutions:        u.ProdExecutions,
				FailedTotalExecutions: u.FailedTotalExecutions,
				FailedProdExecutions:  u.FailedProdExecutions,
				FailureRatePct:        u.FailureRatePct,
				RunTimeAvgSeconds:     u.RunTimeAvgSeconds,
			})
		}
		return users, nil
	}

	lcUsers, err := getLibreChatUsers()
	if err != nil {
		return nil, err
	}
	for _, u := range lcUsers {
		id := u.ID
		if id == "" {
			id = u.Email
		}
		users = append(users, GestionUser{
			ID:                 id,
			Email:              u.Email,
			Name:               u.Name,
			Role:               u.Role,
			Company:            u.Company,
			CreatedAt:          u.CreatedAt,
			VerificationStatus: u.VerificationStatus,
			CanRetry:           u.CanRetry,
			RemainingAttempts:  u.RemainingAttempts,
		})
	}
	return users, nil
}

// HandleGestion renders the users management dashboard page shell.
func HandleGestion(w http.ResponseWriter, r *http.Request) {
	tab := strings.TrimSpace(r.URL.Query().Get("tab"))
	if tab == "" {
		tab = "n8n"
	}
	switch tab {
	case "n8n", "librechat", "empresas", "estadisticas":
	default:
		tab = "n8n"
	}
	meta, err := json.Marshal(map[string]any{
		"companies":      GestionCompaniesList(),
		"defaultCompany": GestionDefaultCompany(),
	})
	metaJS := template.JS(`{"companies":["default"],"defaultCompany":"default"}`)
	if err == nil {
		metaJS = template.JS(meta)
	}
	ui.RenderGestion(w, tab, metaJS)
}

func buildEmpresaRowsForUI() []ui.EmpresaRowView {
	list := GestionCompaniesList()
	def := GestionDefaultCompany()
	out := make([]ui.EmpresaRowView, 0, len(list))
	for _, c := range list {
		masked := CompanyProfileMaskedForName(c)
		row := ui.EmpresaRowView{Name: c, IsDefault: c == def}
		if m, ok := masked.Credentials[ProviderOpenAI]; ok && m.Configured {
			row.OpenAI = m.APIKey
		} else {
			row.OpenAI = "—"
		}
		if m, ok := masked.Credentials[ProviderGoogle]; ok && m.Configured {
			row.Gemini = m.APIKey
		} else {
			row.Gemini = "—"
		}
		out = append(out, row)
	}
	return out
}

// HandleGestionContent renders the inner content (table + form) for HTMX.
// Users are fetched server-side and passed to the template.
func HandleGestionContent(w http.ResponseWriter, r *http.Request) {
	tab := gestionTab(r)
	if tab == "empresas" {
		ui.RenderGestionContentData(w, ui.GestionData{
			Tab:            tab,
			Companies:      GestionCompaniesList(),
			DefaultCompany: GestionDefaultCompany(),
			EmpresaRows:    buildEmpresaRowsForUI(),
			Users:          []GestionUser{},
		})
		return
	}
	users, err := gestionUsersForTab(tab)
	if err != nil {
		prefix := "Error al obtener usuarios de LibreChat: "
		if tab == "n8n" {
			prefix = "Error al obtener usuarios de n8n: "
		}
		ui.RenderGestionContentWithError(w, tab, prefix+err.Error())
		return
	}

	ui.RenderGestionContent(w, tab, users)
}

// HandleGestionSubmit handles POST to create users via HTMX form submission.
// After successful creation, re-renders the content with updated user list.
func HandleGestionSubmit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	tab := r.FormValue("tab")
	if tab == "" {
		tab = "n8n"
	}
	if tab != "n8n" && tab != "librechat" {
		tab = "n8n"
	}

	var err error
	var n8nSentCount, n8nQueuedCount, n8nErrorCount int

	if r.FormValue("csv_data") != "" {
		err = fmt.Errorf("la creación por CSV está deshabilitada")
	} else if r.FormValue("users_json") != "" {
		_, err = createUsersFromJSON(tab, r.FormValue("users_json"))
	} else if tab == "n8n" {
		n8nSentCount, n8nQueuedCount, n8nErrorCount, err = callN8NCreateAPI(r)
	} else {
		err = callLibreChatAPI(r)
	}

	if err != nil {
		ui.RenderGestionContentWithError(w, tab, "Error al crear usuarios: "+err.Error())
		return
	}

	BroadcastUsersUpdate()

	users, err2 := gestionUsersForTab(tab)
	if err2 != nil {
		prefix := "Error al obtener usuarios de LibreChat: "
		if tab == "n8n" {
			prefix = "Error al obtener usuarios de n8n: "
		}
		ui.RenderGestionContentWithError(w, tab, prefix+err2.Error())
		return
	}

	showInviteSent := tab == "n8n"
	ui.RenderGestionContentData(w, ui.GestionData{
		Tab:            tab,
		Users:          users,
		ShowInviteSent: showInviteSent,
		N8NSentCount:   n8nSentCount,
		N8NQueuedCount: n8nQueuedCount,
		N8NErrorCount:  n8nErrorCount,
	})
}

// createUsersFromCSV creates users from CSV string.
func createUsersFromCSV(tab string, csvData string) ([]byte, error) {
	lines := strings.Split(strings.TrimSpace(csvData), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("CSV debe tener al menos una fila de encabezado y una de datos")
	}

	rawUsers := make([]map[string]string, 0)
	for i := 1; i < len(lines); i++ {
		cols := strings.Split(lines[i], ",")
		if len(cols) < 1 || strings.TrimSpace(cols[0]) == "" {
			continue
		}

		user := make(map[string]string)
		user["email"] = strings.TrimSpace(cols[0])
		if len(cols) > 1 {
			user["name"] = strings.TrimSpace(cols[1])
		}
		if len(cols) > 2 {
			user["password"] = strings.TrimSpace(cols[2])
		}
		if len(cols) > 3 {
			user["role"] = strings.TrimSpace(cols[3])
		}
		if len(cols) > 4 {
			user["company"] = strings.TrimSpace(cols[4])
		}
		rawUsers = append(rawUsers, user)
	}

	if len(rawUsers) == 0 {
		return nil, fmt.Errorf("no se encontraron usuarios válidos en el CSV")
	}

	jsonData, err := json.Marshal(rawUsers)
	if err != nil {
		return nil, fmt.Errorf("error al procesar CSV: %w", err)
	}

	return createUsersFromJSON(tab, string(jsonData))
}

// createUsersFromJSON creates users from JSON string (CSV import).
func createUsersFromJSON(tab string, usersJSON string) ([]byte, error) {
	var rawUsers []map[string]string
	if err := json.Unmarshal([]byte(usersJSON), &rawUsers); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	if tab == "n8n" {
		for _, u := range rawUsers {
			co := strings.TrimSpace(u["company"])
			if co != "" && !IsValidGestionCompany(co) {
				return nil, fmt.Errorf("empresa no válida para el email %q", u["email"])
			}
		}

		requests := make([]n8nUserRequest, 0, len(rawUsers))
		for _, u := range rawUsers {
			requests = append(requests, n8nUserRequest{
				Email:   strings.TrimSpace(u["email"]),
				Role:    "global:member",
				Company: strings.TrimSpace(u["company"]),
			})
		}

		result, err := createN8NUsersInternal(requests)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
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
		role = "USER"

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

		co := strings.TrimSpace(u["company"])
		if co == "" {
			co = GestionDefaultCompany()
		}
		if !IsValidGestionCompany(co) {
			results = append(results, result{Email: email, Created: false, Error: "empresa no válida"})
			continue
		}
		if canon, ok := CanonicalGestionCompany(co); ok {
			co = canon
		}

		now := time.Now()
		uIns := lcUser{
			ID:            primitive.NewObjectID(),
			Name:          name,
			Username:      username,
			Email:         email,
			Password:      string(hash),
			Role:          role,
			Company:       co,
			Provider:      "local",
			EmailVerified: true,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		_, err = coll.InsertOne(ctx, uIns)
		if err != nil {
			results = append(results, result{Email: email, Created: false, Error: "failed to insert user"})
			continue
		}
		if err := SyncLibreChatUserProviderKeys(ctx, client, uIns.ID, co); err != nil {
			log.Printf("gestion: sincronizar keys LibreChat (import) %s: %v", email, err)
		}

		results = append(results, result{Email: email, Created: true})
	}

	return json.Marshal(results)
}

// fetchN8NUsersStruct fetches n8n users and returns as structured slice.
func fetchN8NUsersStruct() ([]GestionUser, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	cookies, err := n8nOwnerCookies(client)
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

	// n8n API returns {"data": {"count": N, "items": [...]}}
	type n8nUser struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"firstName"`
		Role  string `json:"role"`
	}

	// Try parsing as {"data": {"count": N, "items": [...]}} (paginated response)
	var paginatedResponse struct {
		Data struct {
			Count int       `json:"count"`
			Items []n8nUser `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &paginatedResponse); err == nil && len(paginatedResponse.Data.Items) > 0 {
		users := make([]GestionUser, 0, len(paginatedResponse.Data.Items))
		for _, u := range paginatedResponse.Data.Items {
			users = append(users, GestionUser{
				ID:    u.ID,
				Email: u.Email,
				Name:  u.Name,
				Role:  u.Role,
			})
		}
		return users, nil
	}

	// Try parsing as {"data": [...]} (array of users)
	var arrayResponse struct {
		Data []n8nUser `json:"data"`
	}
	if err := json.Unmarshal(data, &arrayResponse); err == nil && len(arrayResponse.Data) > 0 {
		users := make([]GestionUser, 0, len(arrayResponse.Data))
		for _, u := range arrayResponse.Data {
			users = append(users, GestionUser{
				ID:    u.ID,
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
			ID:    singleResponse.Data.ID,
			Email: singleResponse.Data.Email,
			Name:  singleResponse.Data.Name,
			Role:  singleResponse.Data.Role,
		}}, nil
	}

	log.Printf("n8n users parse error, raw response: %s", string(data))
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

	defCo := GestionDefaultCompany()
	users := make([]GestionUser, 0, len(lcUsers))
	for _, u := range lcUsers {
		co := strings.TrimSpace(u.Company)
		if co == "" {
			co = defCo
		}
		users = append(users, GestionUser{
			ID:      u.ID.Hex(),
			Email:   u.Email,
			Name:    u.Name,
			Role:    u.Role,
			Company: co,
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
	if tab == "empresas" || tab == "estadisticas" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if tab == "n8n" {
		n8nUsers, err := getN8NUsers()
		if err != nil {
			log.Printf("gestion users-rows n8n: %v", err)
			ui.RenderGestionRows(w, tab, `<div class="table-responsive-wrap"><table class="data-table"><tbody><tr><td colspan="7" class="empty-state empty-state-error"><p>No se pudieron cargar los usuarios.</p><p class="empty-state-sub">Revisa credenciales de n8n y la red.</p></td></tr></tbody></table></div>`)
			return
		}
		ui.RenderGestionRows(w, tab, buildN8NUsersTableHTML(n8nUsers))
		return
	}

	lcList, err := getLibreChatUsers()
	if err != nil {
		log.Printf("gestion users-rows librechat: %v", err)
		ui.RenderGestionRows(w, tab, `<div class="table-responsive-wrap"><table class="data-table"><tbody><tr><td colspan="6" class="empty-state empty-state-error"><p>No se pudieron cargar los usuarios.</p><p class="empty-state-sub">Comprueba MongoDB y variables de entorno.</p></td></tr></tbody></table></div>`)
		return
	}
	gu := make([]GestionUser, 0, len(lcList))
	for _, u := range lcList {
		id := u.ID
		if id == "" {
			id = u.Email
		}
		gu = append(gu, GestionUser{
			ID:                 id,
			Email:              u.Email,
			Name:               u.Name,
			Role:               u.Role,
			Company:            u.Company,
			VerificationStatus: u.VerificationStatus,
			CanRetry:           u.CanRetry,
			RemainingAttempts:  u.RemainingAttempts,
		})
	}
	ui.RenderGestionRows(w, tab, buildLibreChatUsersTableHTML(gu))
}

// Helper: fetch n8n users (with per-user stats) as JSON for HTMX fragments.
func fetchN8NUsers() ([]byte, error) {
	users, err := getN8NUsers()
	if err != nil {
		return nil, err
	}
	return json.Marshal(users)
}

// buildN8NUsersTableHTML renders the users table for HTMX (tab n8n).
func buildN8NUsersTableHTML(users []N8NUser) string {
	var b strings.Builder
	b.WriteString(`<div class="table-responsive-wrap"><table class="data-table gestion-table-n8n"><thead><tr><th>Email</th><th>Nombre</th><th>Apellido(s)</th><th>Rol</th><th>Status</th><th>Empresa</th><th>Acciones</th></tr></thead><tbody>`)
	if len(users) == 0 {
		b.WriteString(`<tr><td colspan="7" class="empty-state empty-state-soft"><div class="empty-state-icon" aria-hidden="true">👤</div><p>Aún no hay usuarios en n8n</p><p class="empty-state-sub"><strong>Nuevo usuario</strong> o la consola de n8n.</p></td></tr>`)
		b.WriteString(`</tbody></table></div>`)
		return b.String()
	}
	for _, u := range users {
		fn := strings.TrimSpace(u.FirstName)
		if fn == "" {
			fn = "—"
		}
		ln := strings.TrimSpace(u.LastName)
		if ln == "" {
			ln = "—"
		}
		roleClass := "role-user"
		if u.Role == "ADMIN" || u.Role == "global:admin" || u.Role == "global:owner" {
			roleClass = "role-admin"
		}
		roleLabel := u.Role
		if u.IsPending {
			roleLabel += " (pendiente)"
		}
		n8nStatus := computeN8NVerificationStatus(u)
		invite := ""
		if u.InviteAcceptURL != "" {
			invite = fmt.Sprintf(`<button type="button" class="btn-small btn-invite" onclick="openInviteModalDirect('%s','%s')">Invitar</button> `,
				html.EscapeString(u.Email), html.EscapeString(u.InviteAcceptURL))
		}
		uid := u.ID
		if uid == "" {
			uid = u.Email
		}
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td><span class="role-badge %s">%s</span></td><td><span class="verification-badge verification-%s">%s</span></td><td>%s</td><td>%s<button type="button" class="btn-small btn-edit" data-tab="n8n" data-id="%s" data-email="%s" data-firstname="%s" data-lastname="%s" data-role="%s" data-company="%s" onclick="openEditModalFromDataset(this)">Editar</button> <button type="button" class="btn-small btn-delete" data-tab="n8n" data-id="%s" data-email="%s" onclick="deleteUser(this)">Eliminar</button></td></tr>`,
			html.EscapeString(u.Email),
			html.EscapeString(fn),
			html.EscapeString(ln),
			roleClass,
			html.EscapeString(roleLabel),
			html.EscapeString(n8nStatus),
			html.EscapeString(n8nStatus),
			html.EscapeString(u.Company),
			invite,
			html.EscapeString(uid),
			html.EscapeString(u.Email),
			html.EscapeString(u.FirstName),
			html.EscapeString(u.LastName),
			html.EscapeString(u.Role),
			html.EscapeString(u.Company),
			html.EscapeString(uid),
			html.EscapeString(u.Email),
		)
	}
	b.WriteString(`</tbody></table></div>`)
	return b.String()
}

// buildLibreChatUsersTableHTML renders the users table for HTMX (tab librechat).
func buildLibreChatUsersTableHTML(users []GestionUser) string {
	var b strings.Builder
	b.WriteString(`<div class="table-responsive-wrap"><table class="data-table"><thead><tr><th>Email</th><th>Nombre</th><th>Rol</th><th>Status</th><th>Empresa</th><th>Acciones</th></tr></thead><tbody>`)
	if len(users) == 0 {
		b.WriteString(`<tr><td colspan="6" class="empty-state empty-state-soft"><div class="empty-state-icon" aria-hidden="true">💬</div><p>No hay cuentas de LibreChat</p><p class="empty-state-sub">Usa <strong>Nuevo usuario</strong> en esta pestaña para dar de alta el primer acceso.</p></td></tr>`)
		b.WriteString(`</tbody></table></div>`)
		return b.String()
	}
	for _, u := range users {
		name := strings.TrimSpace(u.Name)
		if name == "" {
			name = "-"
		}
		roleClass := "role-user"
		if u.Role == "ADMIN" {
			roleClass = "role-admin"
		}
		uid := u.ID
		if uid == "" {
			uid = u.Email
		}
		actions := fmt.Sprintf(`<button type="button" class="btn-small btn-edit" data-tab="librechat" data-id="%s" data-email="%s" data-name="%s" data-role="%s" data-company="%s" onclick="openEditModalFromDataset(this)">Editar</button>`,
			html.EscapeString(uid),
			html.EscapeString(u.Email),
			html.EscapeString(name),
			html.EscapeString(u.Role),
			html.EscapeString(u.Company),
		)
		if u.CanRetry {
			actions += fmt.Sprintf(` <button type="button" class="btn-small btn-retry" data-email="%s" onclick="retryVerification('%s')">Reintentar (%d)</button>`,
				html.EscapeString(u.Email),
				html.EscapeString(u.Email),
				u.RemainingAttempts,
			)
		}
		actions += fmt.Sprintf(` <button type="button" class="btn-small btn-delete" data-tab="librechat" data-id="%s" data-email="%s" onclick="deleteUser(this)">Eliminar</button>`,
			html.EscapeString(uid),
			html.EscapeString(u.Email),
		)
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td><span class="role-badge %s">%s</span></td><td><span class="verification-badge verification-%s">%s</span></td><td>%s</td><td>%s</td></tr>`,
			html.EscapeString(u.Email),
			html.EscapeString(name),
			roleClass,
			html.EscapeString(u.Role),
			html.EscapeString(u.VerificationStatus),
			html.EscapeString(u.VerificationStatus),
			html.EscapeString(u.Company),
			actions,
		)
	}
	b.WriteString(`</tbody></table></div>`)
	return b.String()
}

// callN8NCreateAPI crea usuarios n8n desde el formulario de gestión
// reutilizando la misma lógica central que POST /admin/n8n/users.
func callN8NCreateAPI(r *http.Request) (int, int, int, error) {
	emails := r.Form["email"]

	if len(emails) == 0 {
		return 0, 0, 0, fmt.Errorf("no users to create")
	}

	company := strings.TrimSpace(r.FormValue("company"))
	if company == "" {
		company = GestionDefaultCompany()
	}
	if !IsValidGestionCompany(company) {
		return 0, 0, 0, fmt.Errorf("empresa no válida")
	}

	requests := make([]n8nUserRequest, 0, len(emails))
	for _, email := range emails {
		e := strings.TrimSpace(email)
		if e == "" {
			continue
		}
		requests = append(requests, n8nUserRequest{Email: e, Role: "global:member", Company: company})
	}
	if len(requests) == 0 {
		return 0, 0, 0, fmt.Errorf("no users to create")
	}

	result, err := createN8NUsersInternal(requests)
	if err != nil {
		return 0, 0, 0, err
	}

	data, _ := json.Marshal(result)
	var out struct {
		Deliveries []n8nEmailDeliveryResult `json:"deliveries"`
	}
	_ = json.Unmarshal(data, &out)

	var errs []string
	for _, d := range out.Deliveries {
		if d.Error != "" {
			errs = append(errs, fmt.Sprintf("%s: %s", d.Email, d.Error))
		}
	}
	if len(errs) > 0 {
		return 0, 0, len(errs), fmt.Errorf("%s", strings.Join(errs, "; "))
	}

	sent := 0
	queued := 0
	for _, d := range out.Deliveries {
		if d.Sent {
			sent++
		}
		if d.Queued {
			queued++
		}
	}
	if queued > 0 {
		log.Printf("gestion n8n: %d invitaciones en cola por verificación SES", queued)
	}

	return sent, queued, 0, nil
}

// callLibreChatAPI crea usuarios LibreChat desde el formulario de gestión reutilizando la misma lógica que POST /admin/librechat/users.
func callLibreChatAPI(r *http.Request) error {
	emails := r.Form["email"]
	names := r.Form["name"]
	passwords := r.Form["password"]

	if len(emails) == 0 {
		return fmt.Errorf("no users to create")
	}

	company := strings.TrimSpace(r.FormValue("company"))
	if company == "" {
		company = GestionDefaultCompany()
	}

	requests := make([]createUserRequest, 0, len(emails))
	for i, email := range emails {
		name := email
		if i < len(names) && names[i] != "" {
			name = names[i]
		}
		password := ""
		if i < len(passwords) {
			password = passwords[i]
		}
		requests = append(requests, createUserRequest{
			Email:    email,
			Name:     name,
			Password: password,
			Role:     "USER",
			Company:  company,
		})
	}

	results, err := createLibreChatUsersInternal(requests)
	if err != nil {
		return err
	}
	var errs []string
	for _, res := range results {
		if res.Error != "" {
			errs = append(errs, fmt.Sprintf("%s: %s", res.Email, res.Error))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func getVerificationStatusForEmail(email string) string {
	state, err := emailpkg.GetVerificationState(email)
	if err != nil || state.Status == "" {
		return ""
	}
	return string(state.Status)
}

func getVerificationRetryInfo(email string) (bool, int) {
	canRetry, remaining, err := emailpkg.CanRetry(email)
	if err != nil {
		return false, 0
	}
	return canRetry, remaining
}

func computeN8NVerificationStatus(u N8NUser) string {
	if u.IsPending && u.InviteAcceptURL != "" {
		return "invited"
	}
	if u.IsPending {
		return "pending"
	}
	return "active"
}
