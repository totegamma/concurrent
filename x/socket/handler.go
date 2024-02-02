// Package socket is used for handling user streaming socket
package socket

import (
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
)

// Handler is the interface for handling websocket
type Handler interface {
	Connect(c echo.Context) error
	CurrentConnectionCount() int64
}

type handler struct {
	service Service
	manager Manager
	rdb     *redis.Client
	mutex   *sync.Mutex
	counter int64
}

// NewHandler creates a new handler
func NewHandler(service Service, rdb *redis.Client, manager Manager) Handler {
	return &handler{
		service,
		manager,
		rdb,
		&sync.Mutex{},
		0,
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (h *handler) CurrentConnectionCount() int64 {
	return atomic.LoadInt64(&h.counter)
}

func (h *handler) send(ws *websocket.Conn, message string) error {
	h.mutex.Lock() // XXX: このロックってなんで必要なんだっけ？
	defer h.mutex.Unlock()
	return ws.WriteMessage(websocket.TextMessage, []byte(message))
}

// Connect is used for start websocket connection
func (h *handler) Connect(c echo.Context) error {
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		slog.Error(
			"Failed to upgrade WebSocket",
			slog.String("error", err.Error()),
			slog.String("module", "socket"),
		)
	}
	defer func() {
		h.manager.Unsubscribe(ws)
		ws.Close()
	}()

	ctx := c.Request().Context()

	pubsub := h.rdb.Subscribe(ctx)
	defer pubsub.Close()

	psch := pubsub.Channel()
	quit := make(chan struct{})

	atomic.AddInt64(&h.counter, 1)
	defer atomic.AddInt64(&h.counter, -1)

	go func() {
		for {
			select {
			case <-quit:
				slog.InfoContext(
					ctx, "Socket closed",
					slog.String("module", "socket"),
				)
				return
			case msg := <-psch:

				if msg == nil {
					slog.WarnContext(
						ctx, "received nil message",
						slog.String("module", "socket"),
					)
					return
				}

				slog.DebugContext(
					ctx, fmt.Sprintf("Socket message: %s", msg.Payload[:64]),
					slog.String("module", "socket"),
				)

				err = h.send(ws, msg.Payload)
				if err != nil {
					slog.ErrorContext(
						ctx, "Error writing message",
						slog.String("error", err.Error()),
						slog.String("module", "socket"),
					)
					return
				}
			}
		}
	}()

	for {
		var req Request
		err := ws.ReadJSON(&req)
		if err != nil {
			slog.ErrorContext(
				ctx, "Error reading JSON",
				slog.String("error", err.Error()),
				slog.String("module", "socket"),
			)
			break
		}

		if req.Type == "ping" {
			err = h.send(ws, "{\"type\":\"pong\"}")
			if err != nil {
				slog.ErrorContext(
					ctx, "Error writing message",
					slog.String("error", err.Error()),
					slog.String("module", "socket"),
				)
				break
			}
			continue
		}

		slog.DebugContext(
			ctx, fmt.Sprintf("Socket subscribe: %s", req.Channels),
			slog.String("module", "socket"),
		)
		pubsub.Unsubscribe(ctx)
		pubsub.Subscribe(ctx, req.Channels...)
		h.manager.Subscribe(ws, req.Channels)
	}

	close(quit)

	return nil
}
