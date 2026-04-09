package admin

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type SSEClient struct {
	ID   string
	Send chan []byte
	Done chan struct{}
}

type Broadcaster struct {
	mu         sync.RWMutex
	clients    map[string]SSEClient
	register   chan SSEClient
	unregister chan string
	broadcast  chan []byte
}

var globalBroadcaster = NewBroadcaster()

func NewBroadcaster() *Broadcaster {
	b := &Broadcaster{
		clients:    make(map[string]SSEClient),
		register:   make(chan SSEClient),
		unregister: make(chan string),
		broadcast:  make(chan []byte, 10),
	}
	go b.Run()
	return b
}

func (b *Broadcaster) Run() {
	for {
		select {
		case client := <-b.register:
			b.mu.Lock()
			b.clients[client.ID] = client
			b.mu.Unlock()
			log.Printf("SSE: client connected: %s", client.ID)

		case id := <-b.unregister:
			b.mu.Lock()
			if client, ok := b.clients[id]; ok {
				close(client.Send)
				close(client.Done)
				delete(b.clients, id)
				log.Printf("SSE: client disconnected: %s", id)
			}
			b.mu.Unlock()

		case msg := <-b.broadcast:
			b.mu.RLock()
			for _, client := range b.clients {
				select {
				case client.Send <- msg:
				default:
					log.Printf("SSE: failed to send to client %s, removing", client.ID)
					close(client.Send)
					close(client.Done)
					delete(b.clients, client.ID)
				}
			}
			b.mu.RUnlock()
		}
	}
}

func (b *Broadcaster) Broadcast(event string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Printf("SSE: failed to marshal broadcast data: %v", err)
		return
	}

	message := fmt.Sprintf("event: %s\ndata: %s\n\n", event, jsonData)
	b.broadcast <- []byte(message)
}

func (b *Broadcaster) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

type UsersData struct {
	N8N       []N8NUser       `json:"n8n"`
	LibreChat []LibreChatUser `json:"librechat"`
}

func HandleGestionStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	clientID := fmt.Sprintf("%d", time.Now().UnixNano())
	client := SSEClient{
		ID:   clientID,
		Send: make(chan []byte, 10),
		Done: make(chan struct{}),
	}

	globalBroadcaster.register <- client
	defer func() {
		globalBroadcaster.unregister <- clientID
	}()

	n8nUsers, err := getN8NUsers()
	if err != nil {
		log.Printf("SSE: failed to get n8n users: %v", err)
		n8nUsers = []N8NUser{}
	}
	for i := range n8nUsers {
		n8nUsers[i].VerificationStatus = computeN8NVerificationStatus(n8nUsers[i])
	}

	lcUsers, err := getLibreChatUsers()
	if err != nil {
		log.Printf("SSE: failed to get librechat users: %v", err)
		lcUsers = []LibreChatUser{}
	}

	initialData := UsersData{
		N8N:       n8nUsers,
		LibreChat: lcUsers,
	}

	initialJSON, err := json.Marshal(initialData)
	if err != nil {
		log.Printf("SSE: failed to marshal initial data: %v", err)
		return
	}

	fmt.Fprintf(w, "event: init\ndata: %s\n\n", initialJSON)
	flusher.Flush()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case msg := <-client.Send:
			fmt.Fprint(w, string(msg))
			flusher.Flush()

		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()

		case <-r.Context().Done():
			return

		case <-client.Done:
			return
		}
	}
}

func BroadcastUsersUpdate() {
	n8nUsers, err := getN8NUsers()
	if err != nil {
		log.Printf("SSE: failed to get n8n users for broadcast: %v", err)
		n8nUsers = []N8NUser{}
	}
	for i := range n8nUsers {
		n8nUsers[i].VerificationStatus = computeN8NVerificationStatus(n8nUsers[i])
	}

	lcUsers, err := getLibreChatUsers()
	if err != nil {
		log.Printf("SSE: failed to get librechat users for broadcast: %v", err)
		lcUsers = []LibreChatUser{}
	}
	data := UsersData{
		N8N:       n8nUsers,
		LibreChat: lcUsers,
	}

	globalBroadcaster.Broadcast("users_updated", data)
	log.Printf("SSE: broadcast users_updated to %d clients", globalBroadcaster.ClientCount())
}
