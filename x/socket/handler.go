// Package socket is used for handling user streaming socket
package socket

import (
    "log"
	"context"
    "net/http"
    "github.com/gorilla/websocket"
    "github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

// Handler is handles websocket
type Handler struct {
    service *Service
    rdb* redis.Client
}

// NewHandler is used for wire.go
func NewHandler(service *Service, rdb *redis.Client) *Handler {
    return &Handler{service, rdb}
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
        ws.Close()
    }()

    for {
        var req ChannelRequest
        err := ws.ReadJSON(&req)
        if err != nil {
            log.Println("Error reading JSON: ", err)
            break
        }

        // Unsubscribe from all channels before subscribing to new ones
        pubsub := h.rdb.Subscribe(ctx)
        pubsub.Unsubscribe(ctx)

        // Subscribe to new channels
        for _, ch := range req.Channels {
            pubsub.Subscribe(ctx, ch)
            log.Printf("Subscribed to channel: %s\n", ch)
        }

        // Read from channels
        go func() {
            for {
                msg, err := pubsub.ReceiveMessage(ctx)
                if err != nil {
                    log.Println("Error receiving message: ", err)
                    break
                }
                log.Printf("Received message from channel %s: %s\n", msg.Channel, msg.Payload)

                err = ws.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
                if err != nil {
                    log.Println("Error writing message: ", err)
                    break
                }
            }
        }()
    }
    return nil
}

