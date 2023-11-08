// Package socket is used for handling user streaming socket
package socket

import (
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"log"
	"net/http"
	"sync"
)

// Handler is the interface for handling websocket
type Handler interface {
	Connect(c echo.Context) error
}

type handler struct {
	service Service
	manager Manager
	rdb     *redis.Client
	mutex   *sync.Mutex
}

// NewHandler creates a new handler
func NewHandler(service Service, rdb *redis.Client, manager Manager) Handler {
	return &handler{
		service,
		manager,
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

func (h handler) send(ws *websocket.Conn, message string) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return ws.WriteMessage(websocket.TextMessage, []byte(message))
}

// Connect is used for start websocket connection
func (h handler) Connect(c echo.Context) error {
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		log.Println("Failed to upgrade WebSocket:", err)
		c.Logger().Error(err)
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

	go func() {
		for {
			select {
			case <-quit:
				log.Println("[socket] closed")
				return
			case msg := <-psch:
				log.Printf("[socket] -> %s\n", msg.Payload[:64])
				err = h.send(ws, msg.Payload)
				if err != nil {
					log.Println("Error writing message: ", err)
					return
				}
			}
		}
	}()

	for {
		var req Request
		err := ws.ReadJSON(&req)
		if err != nil {
			log.Println("Error reading JSON: ", err)
			break
		}

		if req.Type == "ping" {
			err = h.send(ws, "{\"type\":\"pong\"}")
			if err != nil {
				log.Println("Error writing message: ", err)
				break
			}
			continue
		}

		log.Printf("[socket] subscribe: %s\n", req.Channels)
		pubsub.Unsubscribe(ctx)
		pubsub.Subscribe(ctx, req.Channels...)
		h.manager.Subscribe(ws, req.Channels)
	}

	close(quit)

	return nil
}
