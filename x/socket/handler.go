// Package socket is used for handling user streaming socket
package socket

import (
    "log"
    "sync"
	"context"
    "net/http"
    "github.com/gorilla/websocket"
    "github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

// Handler is handles websocket
type Handler struct {
    service *Service
    rdb *redis.Client
    mutex *sync.Mutex
}

// NewHandler is used for wire.go
func NewHandler(service *Service, rdb *redis.Client) *Handler {
    return &Handler{
        service,
        rdb,
        &sync.Mutex{},
    }
}

var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

func (h Handler) send(ws *websocket.Conn, message string) error {
    h.mutex.Lock()
    defer h.mutex.Unlock()
    return ws.WriteMessage(websocket.TextMessage, []byte(message))
}


// Connect is used for start websocket connection
func (h Handler) Connect(c echo.Context) error {
    ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
    if err != nil {
        log.Println("Failed to upgrade WebSocket:", err)
        c.Logger().Error(err)
    }
    defer ws.Close()

    var ctx context.Context
    var cancel context.CancelFunc
    var pubsub *redis.PubSub

    for {
        var req ChannelRequest
        err := ws.ReadJSON(&req)
        if err != nil {
            log.Println("Error reading JSON: ", err)
            break
        }

        if cancel != nil {
            cancel()
        }
        if pubsub != nil {
            pubsub.Close()
        }

        ctx, cancel = context.WithCancel(context.Background())

        // Unsubscribe from all channels before subscribing to new ones
        pubsub = h.rdb.Subscribe(ctx)

        // Subscribe to new channels
        for _, ch := range req.Channels {
            pubsub.Subscribe(ctx, ch)
            log.Printf("Subscribed to channel: %s\n", ch)
        }

        // Read from channels
        go func(ctx context.Context, pubsub *redis.PubSub) {
            for {
                select {
                case <-ctx.Done():
                    log.Println("finish goroutine")
                    return
                default:
                    msg, err := pubsub.ReceiveMessage(ctx)
                    if ctx.Err() != nil {
                        log.Println("context seems to be canceled")
                        continue
                    }
                    if err != nil {
                        log.Println("Error receiving message: ", err)
                        break
                    }
                    log.Printf("Received message from channel %s: %s\n", msg.Channel, msg.Payload)

                    err = h.send(ws, msg.Payload)
                    if err != nil {
                        log.Println("Error writing message: ", err)
                        break
                    }
                }
            }
        }(ctx, pubsub)

    }

    defer func() {
        if cancel != nil {
            cancel()
        }
        if pubsub != nil {
            pubsub.Close()
        }
    }()

    return nil
}

