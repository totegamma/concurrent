package socket

import (
    "log"
    "sync"
    "github.com/gorilla/websocket"
)

type SocketService struct {
    clients map[*websocket.Conn]bool
    clientsMutex *sync.Mutex
}

func NewSocketService() *SocketService {
    return &SocketService{
        make(map[*websocket.Conn]bool),
        &sync.Mutex{},
    }
}

func (s *SocketService) AddClient(ws *websocket.Conn) {
    s.clientsMutex.Lock()
    s.clients[ws] = true
    s.clientsMutex.Unlock()

}

func (s *SocketService) RemoveClient(ws *websocket.Conn) {
    s.clientsMutex.Lock()
    delete(s.clients, ws)
    s.clientsMutex.Unlock()
    ws.Close()
}

func (s *SocketService) NotifyAllClients(message []byte) {
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
