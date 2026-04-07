package main

import (
	"log"
	"net/http"

	"launcher/admin"
	"launcher/auth"
	"launcher/config"
	"launcher/email"
	"launcher/services"
	"launcher/ui"
)

func main() {
	if err := admin.InitCompanyStore(); err != nil {
		log.Printf("gestión empresas (JSON): %v", err)
	}

	if err := email.LoadVerificationStore(); err != nil {
		log.Printf("verificación email (JSON): %v", err)
	}
	email.StartVerificationWorkerPool(2)

	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("GET /auth/check", auth.HandleCheck)
	mux.HandleFunc("GET /auth/login", func(w http.ResponseWriter, r *http.Request) {
		auth.HandleLogin(w, r, ui.RenderLogin)
	})
	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		auth.HandleLogin(w, r, ui.RenderLogin)
	})
	mux.HandleFunc("GET /auth/logout", auth.HandleLogout)

	// Services
	mux.HandleFunc("GET /services/{service}/ready", services.HandleReady)
	mux.HandleFunc("GET /services/queue-status", func(w http.ResponseWriter, r *http.Request) {
		services.HandleQueueStatus(w, r, ui.RenderQueueFragment)
	})
	mux.HandleFunc("POST /services/{service}/stop", auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		services.HandleStop(w, r, ui.RenderQueueFragment)
	}))

	// Admin — user management
	mux.HandleFunc("/admin/librechat/users", auth.RequireAuth(admin.HandleLibreChatUsers))
	mux.HandleFunc("/admin/n8n/users", auth.RequireAuth(admin.HandleN8NUsers))
	mux.HandleFunc("POST /admin/n8n/users/password-reset-link", auth.RequireAuth(admin.HandleN8NPasswordResetLink))
	mux.HandleFunc("/admin/verification/retry", auth.RequireAuth(admin.HandleVerificationRetry))

	// Gestion dashboard
	mux.HandleFunc("GET /gestion", auth.RequireAuth(admin.HandleGestion))
	mux.HandleFunc("GET /gestion/content", auth.RequireAuth(admin.HandleGestionContent))
	mux.HandleFunc("POST /gestion", auth.RequireAuth(admin.HandleGestionSubmit))
	mux.HandleFunc("GET /gestion/invite", auth.RequireAuth(admin.HandleInviteModal))
	mux.HandleFunc("GET /gestion/users-rows", auth.RequireAuth(admin.HandleGestionUsersRows))
	mux.HandleFunc("GET /gestion/stream", auth.RequireAuth(admin.HandleGestionStream))
	mux.HandleFunc("GET /gestion/api/stats", auth.RequireAuth(admin.HandleGestionStatsAPI))
	mux.HandleFunc("GET /gestion/charts-content", auth.RequireAuth(admin.HandleGestionChartsContent))
	hCompanies := auth.RequireAuth(admin.HandleGestionCompaniesAPI)
	mux.HandleFunc("GET /gestion/api/companies", hCompanies)
	mux.HandleFunc("POST /gestion/api/companies", hCompanies)
	mux.HandleFunc("PUT /gestion/api/companies", hCompanies)
	mux.HandleFunc("DELETE /gestion/api/companies", hCompanies)
	mux.HandleFunc("PATCH /gestion/api/companies", hCompanies)
	mux.HandleFunc("POST /gestion/api/companies/sync", auth.RequireAuth(admin.HandleGestionCompanyIntegrationsSyncAPI))
	mux.HandleFunc("POST /gestion/api/companies/default", auth.RequireAuth(admin.HandleGestionCompaniesDefaultAPI))

	// Wake (catch-all — called by nginx @launcher on 502)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		services.HandleWake(w, r, ui.RenderWait, ui.RenderQueue)
	})

	// Discover running containers before serving requests.
	services.StartReconciler()

	log.Printf("Launcher escuchando en %s", config.Port)
	log.Fatal(http.ListenAndServe(config.Port, mux))
}
