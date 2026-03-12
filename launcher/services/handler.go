package services

import (
	"net/http"
	"strings"

	"launcher/config"
)

// RenderWaitFn is the signature for ui.RenderWait to avoid a circular import.
type RenderWaitFn func(http.ResponseWriter, string)

// RenderQueueFn is the signature for ui.RenderQueue.
type RenderQueueFn func(http.ResponseWriter, string, int, Status)

// RenderQueueFragmentFn is the signature for ui.RenderQueueFragment.
type RenderQueueFragmentFn func(http.ResponseWriter, Status)

// HandleWake is the catch-all handler called by nginx when a service returns 502.
// It resolves the host to a service name, then either shows the wait page or the
// queue page depending on whether a slot is currently available.
func HandleWake(w http.ResponseWriter, r *http.Request, renderWait RenderWaitFn, renderQueue RenderQueueFn) {
	host := r.Host
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}

	service, ok := config.HostToService[host]
	if !ok {
		http.Error(w, "Servicio no encontrado", http.StatusNotFound)
		return
	}

	// Determine position before TriggerStart so we can decide which page to show.
	statusBefore := ServiceStatus(service)

	TriggerStart(service)

	statusAfter := ServiceStatus(service)

	// If the service ended up queued, show the queue page.
	if strings.HasPrefix(statusAfter, "queued:") {
		pos := queuePosition(service)
		renderQueue(w, service, pos, GetStatus())
		return
	}

	// Not queued before either — show the wait page normally.
	_ = statusBefore
	renderWait(w, service)
}

// HandleReady is polled by the wait page via HTMX every 3 seconds.
// When the service is running it responds with HX-Redirect so HTMX navigates away.
func HandleReady(w http.ResponseWriter, r *http.Request) {
	service := r.PathValue("service")
	if service == "" {
		http.Error(w, "missing service", http.StatusBadRequest)
		return
	}

	status := ServiceStatus(service)
	if status == "running" {
		// Service is up — redirect the browser to the app.
		w.Header().Set("HX-Redirect", "/")
	}
	// starting, queued, or stopped: keep polling silently.
	w.WriteHeader(http.StatusOK)
}

// HandleQueueStatus returns an HTML fragment with the current queue state.
// Used by HTMX polling on the queue page.
func HandleQueueStatus(w http.ResponseWriter, r *http.Request, renderFragment RenderQueueFragmentFn) {
	renderFragment(w, GetStatus())
}

// HandleStop stops a service (requires auth enforced by middleware), processes
// the queue, and either redirects HTMX to / if the next service started, or
// returns an updated queue-status fragment.
func HandleStop(w http.ResponseWriter, r *http.Request, renderFragment RenderQueueFragmentFn) {
	service := r.PathValue("service")
	if service == "" {
		http.Error(w, "missing service", http.StatusBadRequest)
		return
	}

	// Validate that the service is known.
	found := false
	for _, svc := range config.HostToService {
		if svc == service {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "servicio desconocido", http.StatusBadRequest)
		return
	}

	// Stop the service. processQueue is called inside StopService.
	StopService(service)

	// If the first queued service has now started, redirect to /.
	// (processQueue already moved it to active with starting=true)
	renderFragment(w, GetStatus())
}

// queuePosition returns the 1-based position of service in the queue (0 = not queued).
func queuePosition(service string) int {
	mu.Lock()
	defer mu.Unlock()
	for i, e := range queue {
		if e.Service == service {
			return i + 1
		}
	}
	return 0
}
