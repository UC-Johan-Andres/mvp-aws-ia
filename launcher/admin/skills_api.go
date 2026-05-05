package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func gestionSkillsAPIPayload(ok bool) map[string]any {
	skills := GetAllSkills()
	companies := GestionCompaniesList()

	companySkillsMap := make(map[string][]map[string]interface{})
	for _, company := range companies {
		companySkills := GetSkillsForCompany(company)
		skillsData := make([]map[string]interface{}, 0, len(companySkills))
		for _, s := range companySkills {
			skillsData = append(skillsData, map[string]interface{}{
				"id":          s.ID,
				"name":        s.Name,
				"type":        s.Type,
				"description": s.Description,
				"isPublic":    s.IsPublic,
			})
		}
		companySkillsMap[company] = skillsData
	}

	defaultSkillIDs := GetDefaultSkills()

	skillsData := make([]map[string]interface{}, 0, len(skills))
	for _, s := range skills {
		skillsData = append(skillsData, map[string]interface{}{
			"id":             s.ID,
			"name":           s.Name,
			"type":           s.Type,
			"description":    s.Description,
			"sourceId":       s.SourceID,
			"mcpServerNames": s.McpServerNames,
			"isPublic":       s.IsPublic,
		})
	}

	out := map[string]any{
		"skills":              skillsData,
		"companySkills":       companySkillsMap,
		"defaultSkills":       defaultSkillIDs,
		"companies":           companies,
		"defaultCompany":      GestionDefaultCompany(),
		"migrationsCompleted": IsMigrationCompleted(),
	}
	if ok {
		out["ok"] = true
	}
	return out
}

func HandleGestionSkillsAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonOK(w, gestionSkillsAPIPayload(false))

	case http.MethodPost:
		var body struct {
			Action    string `json:"action"`
			SkillID   string `json:"skillId"`
			Company   string `json:"company"`
			SkillName string `json:"skillName"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "JSON inválido", http.StatusBadRequest)
			return
		}

		switch body.Action {
		case "associate":
			if strings.TrimSpace(body.SkillID) == "" || strings.TrimSpace(body.Company) == "" {
				jsonError(w, "falta skillId o company", http.StatusBadRequest)
				return
			}

			skill, found := GetSkillByID(body.SkillID)
			if !found {
				jsonError(w, "skill no encontrado", http.StatusNotFound)
				return
			}

			if err := AddSkillToCompany(body.SkillID, body.Company); err != nil {
				jsonError(w, err.Error(), http.StatusBadRequest)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()

			err := SyncSkillToLibreChat(ctx, skill, body.Company)
			if err != nil {
				jsonError(w, "sync error: "+err.Error(), http.StatusInternalServerError)
				return
			}

			groupID, _ := GetCompanyGroupID(body.Company)
			SaveCompanyGroupID(body.Company, groupID)

			GoSyncCompanySkills(body.Company)
			BroadcastUsersUpdate()
			jsonOK(w, gestionSkillsAPIPayload(true))

		case "dissociate":
			if strings.TrimSpace(body.SkillID) == "" || strings.TrimSpace(body.Company) == "" {
				jsonError(w, "falta skillId o company", http.StatusBadRequest)
				return
			}

			skill, found := GetSkillByID(body.SkillID)
			if !found {
				jsonError(w, "skill no encontrado", http.StatusNotFound)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()

			err := UnsyncSkillFromCompany(ctx, skill, body.Company)
			if err != nil {
				jsonError(w, "unsync error: "+err.Error(), http.StatusInternalServerError)
				return
			}

			err = RemoveSkillFromCompany(body.SkillID, body.Company)
			if err != nil {
				jsonError(w, err.Error(), http.StatusBadRequest)
				return
			}

			GoSyncCompanySkills(body.Company)
			BroadcastUsersUpdate()
			jsonOK(w, gestionSkillsAPIPayload(true))

		case "addDefault":
			if strings.TrimSpace(body.SkillID) == "" {
				jsonError(w, "falta skillId", http.StatusBadRequest)
				return
			}

			skill, found := GetSkillByID(body.SkillID)
			if !found {
				jsonError(w, "skill no encontrado", http.StatusNotFound)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()

			err := CreatePublicSkill(ctx, skill)
			if err != nil {
				jsonError(w, "create public skill error: "+err.Error(), http.StatusInternalServerError)
				return
			}

			err = AddDefaultSkill(body.SkillID)
			if err != nil {
				jsonError(w, err.Error(), http.StatusBadRequest)
				return
			}

			BroadcastUsersUpdate()
			jsonOK(w, gestionSkillsAPIPayload(true))

		case "removeDefault":
			if strings.TrimSpace(body.SkillID) == "" {
				jsonError(w, "falta skillId", http.StatusBadRequest)
				return
			}

			err := RemoveDefaultSkill(body.SkillID)
			if err != nil {
				jsonError(w, err.Error(), http.StatusBadRequest)
				return
			}

			BroadcastUsersUpdate()
			jsonOK(w, gestionSkillsAPIPayload(true))

		case "sync":
			if strings.TrimSpace(body.Company) == "" {
				jsonError(w, "falta company", http.StatusBadRequest)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()

			err := SyncAllUsersForCompany(ctx, body.Company)
			if err != nil {
				jsonError(w, "sync error: "+err.Error(), http.StatusInternalServerError)
				return
			}

			GoSyncCompanySkills(body.Company)
			BroadcastUsersUpdate()
			jsonOK(w, gestionSkillsAPIPayload(true))

		default:
			jsonError(w, "acción desconocida: "+body.Action, http.StatusBadRequest)
		}

	case http.MethodDelete:
		name := strings.TrimSpace(r.URL.Query().Get("company"))
		if name == "" {
			jsonError(w, "falta parámetro company", http.StatusBadRequest)
			return
		}

		canon, ok := CanonicalGestionCompany(name)
		if !ok {
			jsonError(w, "empresa no encontrada", http.StatusNotFound)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		err := DeleteCompanyGroup(ctx, canon)
		if err != nil {
			jsonError(w, "delete group error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		err = DeleteCompanySkills(ctx, canon)
		if err != nil {
			jsonError(w, "delete company skills error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		err = RemoveCompanyFromSkillStore(canon)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}

		BroadcastUsersUpdate()
		jsonOK(w, gestionSkillsAPIPayload(true))

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func HandleGestionSkillsAgentsListAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	agents, err := listAgentsMongo(ctx)
	if err != nil {
		jsonError(w, "error listing agents: "+err.Error(), http.StatusInternalServerError)
		return
	}

	agentsData := make([]map[string]interface{}, 0, len(agents))
	for _, agent := range agents {
		agentMap := make(map[string]interface{})
		for k, v := range agent {
			if k == "_id" {
				if oid, ok := v.(primitive.ObjectID); ok {
					agentMap["_id"] = oid.Hex()
				}
			} else if k == "mcpServerNames" {
				if names, ok := v.(primitive.A); ok {
					mcpNames := make([]string, 0, len(names))
					for _, n := range names {
						if s, ok := n.(string); ok {
							mcpNames = append(mcpNames, s)
						}
					}
					agentMap["mcpServerNames"] = mcpNames
				}
			} else {
				agentMap[k] = v
			}
		}
		agentsData = append(agentsData, agentMap)
	}

	jsonOK(w, map[string]any{
		"agents": agentsData,
		"count":  len(agentsData),
	})
}

func HandleGestionSkillsMCPServersAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusInternalServerError)
		return
	}

	mcpSkills := GetMCPSkills()

	skillsData := make([]map[string]interface{}, 0, len(mcpSkills))
	for _, s := range mcpSkills {
		skillsData = append(skillsData, map[string]interface{}{
			"id":             s.ID,
			"name":           s.Name,
			"workflowName":   s.WorkflowName,
			"availableInMCP": s.AvailableInMCP,
		})
	}

	jsonOK(w, map[string]any{
		"mcpServers": skillsData,
		"count":      len(skillsData),
	})
}

func HandleGestionSkillsSyncAllAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Company string `json:"company"`
		Force   bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "JSON inválido", http.StatusBadRequest)
		return
	}

	if body.Force {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()

		err := ForceReRunMigration(ctx)
		if err != nil {
			jsonError(w, "force migration error: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if body.Company != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		err := SyncAllUsersForCompany(ctx, body.Company)
		if err != nil {
			jsonError(w, "sync error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		GoSyncCompanySkills(body.Company)
	}

	BroadcastUsersUpdate()
	jsonOK(w, gestionSkillsAPIPayload(true))
}
