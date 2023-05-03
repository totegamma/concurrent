package socket

import (
    "fmt"
    "net/http"
    "github.com/gorilla/websocket"
)

type SocketHandler struct {
    service *SocketService
}

func NewSocketHandler(service *SocketService) *SocketHandler {
    return &SocketHandler{service}
}

var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

func (h *SocketHandler) Handle(w http.ResponseWriter, r *http.Request) {
    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        fmt.Println("Failed to upgrade WebSocket:", err)
        return
    }
    defer func() {
        h.service.RemoveClient(ws)
        ws.Close()
    }()

    h.service.AddClient(ws)

    for {
        _, _, err := ws.ReadMessage()
        if err != nil {
            fmt.Println("Failed to read WebSocket message:", err)
            break
        }
    }
}



