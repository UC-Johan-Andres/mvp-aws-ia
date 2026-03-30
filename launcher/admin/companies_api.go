package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func gestionCompaniesAPIPayload(ok bool) map[string]any {
	list := GestionCompaniesList()
	profiles := make(map[string]CompanyProfileMasked, len(list))
	for _, c := range list {
		profiles[c] = CompanyProfileMaskedForName(c)
	}
	out := map[string]any{
		"companies":      list,
		"profiles":       profiles,
		"defaultCompany": GestionDefaultCompany(),
	}
	if ok {
		out["ok"] = true
	}
	return out
}

// HandleGestionCompaniesAPI CRUD JSON de empresas (n8n + LibreChat).
// GET: companies ([]string), profiles (map nombre → perfil enmascarado). POST crear con credentials opcional.
// PATCH fusionar credenciales; PUT renombrar; DELETE ?name=
func HandleGestionCompaniesAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonOK(w, gestionCompaniesAPIPayload(false))
	case http.MethodPost:
		var body struct {
			Name        string                        `json:"name"`
			Credentials map[string]ProviderCredential `json:"credentials,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "JSON inválido", http.StatusBadRequest)
			return
		}
		if err := AddGestionCompanyWithCredentials(body.Name, body.Credentials); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Igual que PATCH: propagar credenciales a LibreChat/n8n para usuarios ya asignados a esta empresa.
		GoSyncCompanyAIIntegrations(body.Name)
		BroadcastUsersUpdate()
		jsonOK(w, gestionCompaniesAPIPayload(true))
	case http.MethodPatch:
		var body struct {
			Name        string                        `json:"name"`
			Credentials map[string]ProviderCredential `json:"credentials"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "JSON inválido", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			jsonError(w, "falta name", http.StatusBadRequest)
			return
		}
		if body.Credentials == nil {
			jsonError(w, "falta credentials", http.StatusBadRequest)
			return
		}
		if err := MergeGestionCompanyCredentials(body.Name, body.Credentials); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		GoSyncCompanyAIIntegrations(body.Name)
		BroadcastUsersUpdate()
		jsonOK(w, gestionCompaniesAPIPayload(true))
	case http.MethodPut:
		var body struct {
			OldName string `json:"oldName"`
			NewName string `json:"newName"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "JSON inválido", http.StatusBadRequest)
			return
		}
		oldCanon, newCanon, err := RenameGestionCompany(body.OldName, body.NewName)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		_, mongoErr := mongoRenameCompanyOnUsers(ctx, oldCanon, newCanon)
		if mongoErr != nil {
			_, _, revErr := RenameGestionCompany(newCanon, oldCanon)
			if revErr != nil {
				jsonError(w, "error MongoDB y fallo al revertir registro: "+mongoErr.Error()+" / "+revErr.Error(), http.StatusInternalServerError)
				return
			}
			jsonError(w, "error MongoDB: "+mongoErr.Error(), http.StatusBadGateway)
			return
		}
		GoSyncCompanyAIIntegrations(body.NewName)
		BroadcastUsersUpdate()
		jsonOK(w, gestionCompaniesAPIPayload(true))
	case http.MethodDelete:
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			jsonError(w, "falta parámetro name", http.StatusBadRequest)
			return
		}
		canon, ok := CanonicalGestionCompany(name)
		if !ok {
			jsonError(w, "empresa no encontrada", http.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		n, err := countLibreChatUsersWithCompany(ctx, canon)
		if err != nil {
			jsonError(w, "MongoDB: "+err.Error(), http.StatusBadGateway)
			return
		}
		if err := DeleteGestionCompany(name, int(n)); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		BroadcastUsersUpdate()
		jsonOK(w, gestionCompaniesAPIPayload(true))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleGestionCompanyIntegrationsSyncAPI POST {"name"} — fuerza sync LibreChat + n8n para una empresa.
func HandleGestionCompanyIntegrationsSyncAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "JSON inválido", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		jsonError(w, "falta name", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	lc, nn, err := SyncCompanyAIIntegrations(ctx, body.Name)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	jsonOK(w, map[string]any{
		"ok":             true,
		"libreChatUsers": lc,
		"n8nUsers":       nn,
	})
}

// HandleGestionCompaniesDefaultAPI POST {"name"} — empresa predeterminada.
func HandleGestionCompaniesDefaultAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "JSON inválido", http.StatusBadRequest)
		return
	}
	if err := SetGestionDefaultCompany(body.Name); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	BroadcastUsersUpdate()
	jsonOK(w, gestionCompaniesAPIPayload(true))
}
