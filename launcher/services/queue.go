package services

import (
	"log"
	"sync"
	"time"

	"launcher/config"
)

// serviceState tracks when a service was registered and whether it is still starting.
type serviceState struct {
	startedAt time.Time
	starting  bool
}

// QueueEntry holds a service waiting for a free slot.
type QueueEntry struct {
	Service   string
	EnqueuedAt time.Time
}

// Status is the snapshot returned by GetStatus.
type Status struct {
	Running     []string
	Queue       []QueueEntry
	MaxServices int
}

var (
	mu     sync.Mutex
	active = map[string]*serviceState{}
	queue  []QueueEntry
)

// syncActive reconciles the in-memory active map against actual Docker state.
// - Adds services that are running in Docker but not tracked (e.g. after launcher restart).
// - Removes services that were tracked as running but have stopped unexpectedly.
// Must be called with mu held.
func syncActive() {
	for svc := range config.HostToService {
		state, tracked := active[svc]
		if tracked {
			// Only verify services we think are running (not ones currently starting).
			if !state.starting && !IsRunning(svc) {
				delete(active, svc)
			}
		} else {
			if IsRunning(svc) {
				active[svc] = &serviceState{startedAt: time.Now(), starting: false}
			}
		}
	}
}

// TriggerStart ensures the service is started. If all slots are occupied the
// service is added to the waiting queue instead of evicting an existing one.
func TriggerStart(service string) {
	mu.Lock()

	// Sync real Docker state so slot count is always accurate.
	syncActive()

	// Already active (starting or running) — nothing to do.
	if _, exists := active[service]; exists {
		mu.Unlock()
		return
	}

	// Already in queue — nothing to do.
	for _, e := range queue {
		if e.Service == service {
			mu.Unlock()
			return
		}
	}

	// Slots full → enqueue instead of evicting.
	if len(active) >= config.MaxServices {
		queue = append(queue, QueueEntry{Service: service, EnqueuedAt: time.Now()})
		log.Printf("Slot lleno — %s agregado a la cola (posición %d)", service, len(queue))
		mu.Unlock()
		return
	}

	// Slot available — start immediately.
	active[service] = &serviceState{startedAt: time.Now(), starting: true}
	mu.Unlock()

	go doStart(service)
}

// StopService stops the service, removes it from active, and processes the queue.
func StopService(service string) {
	log.Printf("Deteniendo %s...", service)
	if err := StopContainers(service); err != nil {
		log.Printf("Error deteniendo %s: %v", service, err)
	}

	mu.Lock()
	delete(active, service)
	mu.Unlock()

	processQueue()
}

// processQueue checks whether there is capacity to start the next queued service.
// Must NOT be called while holding mu.
func processQueue() {
	mu.Lock()
	if len(queue) == 0 || len(active) >= config.MaxServices {
		mu.Unlock()
		return
	}
	next := queue[0]
	queue = queue[1:]
	active[next.Service] = &serviceState{startedAt: time.Now(), starting: true}
	mu.Unlock()

	log.Printf("Slot libre — iniciando %s desde la cola", next.Service)
	go doStart(next.Service)
}

// doStart calls StartContainers and then marks the service as no longer starting.
// If start fails, the slot is freed and the queue is processed so it doesn't stall.
func doStart(service string) {
	log.Printf("Iniciando %s...", service)
	if err := StartContainers(service); err != nil {
		log.Printf("Error iniciando %s: %v", service, err)
		mu.Lock()
		delete(active, service)
		mu.Unlock()
		processQueue() // free the slot so the next queued service can start
		return
	}
	mu.Lock()
	if state, exists := active[service]; exists {
		state.starting = false
	}
	mu.Unlock()
	log.Printf("%s iniciado", service)
}

// GetStatus returns a snapshot of running services and the queue.
func GetStatus() Status {
	mu.Lock()
	defer mu.Unlock()

	syncActive()

	running := make([]string, 0, len(active))
	for svc := range active {
		running = append(running, svc)
	}

	queueCopy := make([]QueueEntry, len(queue))
	copy(queueCopy, queue)

	return Status{
		Running:     running,
		Queue:       queueCopy,
		MaxServices: config.MaxServices,
	}
}

// ServiceStatus returns a human-readable status for a single service.
// Possible values: "running", "starting", "queued:N" (1-based position), "stopped".
func ServiceStatus(service string) string {
	mu.Lock()
	defer mu.Unlock()

	if state, ok := active[service]; ok {
		if state.starting {
			return "starting"
		}
		return "running"
	}
	for i, e := range queue {
		if e.Service == service {
			return "queued:" + itoa(i+1)
		}
	}
	return "stopped"
}

// itoa converts an int to its decimal string representation without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
