package main

import (
	"log"
	"net/http"

	"launcher/auth"
	"launcher/config"
	"launcher/services"
	"launcher/ui"
)

func main() {
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

	// Wake (catch-all — called by nginx @launcher on 502)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		services.HandleWake(w, r, ui.RenderWait, ui.RenderQueue)
	})

	// Discover running containers before serving requests.
	services.StartReconciler()

	log.Printf("Launcher escuchando en %s", config.Port)
	log.Fatal(http.ListenAndServe(config.Port, mux))
}
