package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// HandleGestionCompaniesAPI CRUD JSON de empresas (n8n + LibreChat).
// GET lista; POST {"name"} crear; PUT {"oldName","newName"} renombrar; DELETE ?name=
func HandleGestionCompaniesAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonOK(w, map[string]any{
			"companies":      GestionCompaniesList(),
			"defaultCompany": GestionDefaultCompany(),
		})
	case http.MethodPost:
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "JSON inválido", http.StatusBadRequest)
			return
		}
		if err := AddGestionCompany(body.Name); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		BroadcastUsersUpdate()
		jsonOK(w, map[string]any{
			"ok":             true,
			"companies":      GestionCompaniesList(),
			"defaultCompany": GestionDefaultCompany(),
		})
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
		BroadcastUsersUpdate()
		jsonOK(w, map[string]any{
			"ok":             true,
			"companies":      GestionCompaniesList(),
			"defaultCompany": GestionDefaultCompany(),
		})
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
		jsonOK(w, map[string]any{
			"ok":             true,
			"companies":      GestionCompaniesList(),
			"defaultCompany": GestionDefaultCompany(),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
	jsonOK(w, map[string]any{
		"ok":             true,
		"defaultCompany": GestionDefaultCompany(),
		"companies":      GestionCompaniesList(),
	})
}
