package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"launcher/ui"
)

// GestionStatsAPIResponse agrupa métricas por producto; ampliable para LibreChat u otros.
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

// GestionStatsLCSection reservado para futuras métricas (mensajes, tokens, sesiones, etc.).
type GestionStatsLCSection struct {
	Available bool   `json:"available"`
	Message   string `json:"message,omitempty"`
	// Users []LibreChatStatsUser `json:"users,omitempty"` // añadir cuando existan consultas
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

// HandleGestionStatsAPI devuelve JSON agregado para gráficos y futuras integraciones.
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
		LibreChat: GestionStatsLCSection{
			Available: false,
			Message:   "Integración pendiente: aquí se podrán exponer métricas por usuario de LibreChat (MongoDB u otros orígenes).",
		},
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
