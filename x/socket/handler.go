// Package socket is used for handling user streaming socket
package socket

import (
    "log"
    "net/http"
    "github.com/gorilla/websocket"
    "github.com/labstack/echo/v4"
)

// Handler is handles websocket
type Handler struct {
    service *Service
}

// NewHandler is used for wire.go
func NewHandler(service *Service) *Handler {
    return &Handler{service}
}

var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

// Connect is used for start websocket connection
func (h Handler) Connect(c echo.Context) error {
    ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
    if err != nil {
        log.Println("Failed to upgrade WebSocket:", err)
        c.Logger().Error(err)
    }
    defer func() {
        h.service.RemoveClient(ws)
        ws.Close()
    }()

    h.service.AddClient(ws)

    for {
        _, _, err := ws.ReadMessage()
        if err != nil {
            c.Logger().Error(err)
            return err
        }
    }
}

