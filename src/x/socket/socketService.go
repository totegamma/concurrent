package socket

import (
    "log"
    "sync"
    "github.com/gorilla/websocket"
)

// Service is socket service
type Service struct {
    clients map[*websocket.Conn]bool
    clientsMutex *sync.Mutex
}

// NewService is for wire.go
func NewService() *Service {
    return &Service{
        make(map[*websocket.Conn]bool),
        &sync.Mutex{},
    }
}

// AddClient addes a connection to broadcast group
func (s *Service) AddClient(ws *websocket.Conn) {
    s.clientsMutex.Lock()
    s.clients[ws] = true
    s.clientsMutex.Unlock()

}

// RemoveClient removes a connection from broadcast group
func (s *Service) RemoveClient(ws *websocket.Conn) {
    s.clientsMutex.Lock()
    delete(s.clients, ws)
    s.clientsMutex.Unlock()
    ws.Close()
}

// NotifyAllClients broadcasts message to all clients
func (s *Service) NotifyAllClients(message []byte) {
    s.clientsMutex.Lock()
    defer s.clientsMutex.Unlock()

    for client := range s.clients {
        err := client.WriteMessage(websocket.TextMessage, message)
        if err != nil {
            log.Printf("Failed to write WebSocket message to client: %v\n", err)
            delete(s.clients, client)
        }
    }
}
