package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"launcher/config"
	"launcher/ui"
)

// GestionStatsAPIResponse agrupa métricas por producto.
type GestionStatsAPIResponse struct {
	Version     int                    `json:"version"`
	GeneratedAt string                 `json:"generatedAt"`
	N8N         GestionStatsN8NSection `json:"n8n"`
	LibreChat   GestionStatsLCSection  `json:"librechat"`
}

type GestionStatsN8NSection struct {
	Available bool                `json:"available"`
	Error     string              `json:"error,omitempty"`
	Users     []N8NStatsSeriesItem `json:"users,omitempty"`
	Totals    N8NStatsTotals       `json:"totals,omitempty"`
}

type N8NStatsTotals struct {
	UserCount            int     `json:"userCount"`
	WorkflowsAccesibles  int64   `json:"workflowsAccesibles"`
	ProdExecutions       int64   `json:"prodExecutions"`
	FailedProdExecutions int64   `json:"failedProdExecutions"`
	AvgFailureRatePct    float64 `json:"avgFailureRatePct"`
	AvgRunTimeSeconds    float64 `json:"avgRunTimeSeconds"`
}

// GestionStatsLCSection métricas LibreChat (MongoDB).
type GestionStatsLCSection struct {
	Available bool                     `json:"available"`
	Error     string                   `json:"error,omitempty"`
	Message   string                   `json:"message,omitempty"`
	Users     []LibreChatStatsSeriesItem `json:"users,omitempty"`
	Totals    LibreChatStatsTotals       `json:"totals,omitempty"`
}

func n8nTotalsFromSeries(users []N8NStatsSeriesItem) N8NStatsTotals {
	var t N8NStatsTotals
	t.UserCount = len(users)
	if len(users) == 0 {
		return t
	}
	var sumRate, sumRun float64
	for _, u := range users {
		t.WorkflowsAccesibles += u.WorkflowsAccesibles
		t.ProdExecutions += u.ProdExecutions
		t.FailedProdExecutions += u.FailedProdExecutions
		sumRate += u.FailureRatePct
		sumRun += u.RunTimeAvgSeconds
	}
	n := float64(len(users))
	t.AvgFailureRatePct = round2(sumRate / n)
	t.AvgRunTimeSeconds = round2(sumRun / n)
	return t
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

func filterN8NStatsByCompany(series []N8NStatsSeriesItem, company string) []N8NStatsSeriesItem {
	if company == "" {
		return series
	}
	out := make([]N8NStatsSeriesItem, 0, len(series))
	for _, row := range series {
		if N8NUserCompany(row.UserID, row.Email) == company {
			out = append(out, row)
		}
	}
	return out
}

func filterLCStatsByCompany(series []LibreChatStatsSeriesItem, company string) []LibreChatStatsSeriesItem {
	if company == "" {
		return series
	}
	out := make([]LibreChatStatsSeriesItem, 0, len(series))
	for _, row := range series {
		if row.Company == company {
			out = append(out, row)
		}
	}
	return out
}

func lcTotalsFromFiltered(users []LibreChatStatsSeriesItem) LibreChatStatsTotals {
	var t LibreChatStatsTotals
	for _, v := range users {
		t.TotalConversations += v.TotalConversations
		t.TotalMessages += v.TotalMessages
	}
	t.UsersWithActivity = len(users)
	return t
}

// HandleGestionStatsAPI devuelve JSON para la pestaña Estadísticas.
func HandleGestionStatsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 28*time.Second)
	defer cancel()

	out := GestionStatsAPIResponse{
		Version:     1,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if strings.TrimSpace(config.MongoURI) == "" {
		out.LibreChat = GestionStatsLCSection{
			Available: false,
			Message:   "Sin MONGO_URI en el launcher no hay métricas de LibreChat.",
		}
	} else {
		lcUsers, lcTotals, err := FetchLibreChatStatsSeries(ctx)
		if err != nil {
			out.LibreChat = GestionStatsLCSection{
				Available: false,
				Error:     err.Error(),
			}
		} else {
			out.LibreChat = GestionStatsLCSection{
				Available: true,
				Users:     lcUsers,
				Totals:    lcTotals,
			}
			if len(lcUsers) == 0 {
				out.LibreChat.Message = "La base aún no tiene conversaciones ni mensajes asociados a usuarios."
			}
		}
	}

	series, err := FetchN8NStatsSeries(ctx)
	if err != nil {
		out.N8N.Available = false
		out.N8N.Error = err.Error()
	} else {
		out.N8N.Available = true
		out.N8N.Users = series
		out.N8N.Totals = n8nTotalsFromSeries(series)
	}

	q := strings.TrimSpace(r.URL.Query().Get("company"))
	showAll := q == "" || strings.EqualFold(q, "all")
	if !showAll {
		canon, ok := config.CanonicalGestionCompany(q)
		if !ok {
			http.Error(w, "empresa inválida", http.StatusBadRequest)
			return
		}
		if out.N8N.Available {
			out.N8N.Users = filterN8NStatsByCompany(out.N8N.Users, canon)
			out.N8N.Totals = n8nTotalsFromSeries(out.N8N.Users)
		}
		if out.LibreChat.Available {
			out.LibreChat.Users = filterLCStatsByCompany(out.LibreChat.Users, canon)
			out.LibreChat.Totals = lcTotalsFromFiltered(out.LibreChat.Users)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

// HandleGestionChartsContent sirve el fragmento HTML del módulo de gráficos.
func HandleGestionChartsContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ui.RenderGestionChartsContent(w)
}
