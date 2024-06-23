// Package message is handles concurrent message objects
package message

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/core"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("message")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
}

type handler struct {
	service core.MessageService
}

// NewHandler creates a new handler
func NewHandler(service core.MessageService) Handler {
	return &handler{service: service}
}

// Get returns an message by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Message.Handler.Get")
	defer span.End()

	id := c.Param("id")

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	var message core.Message
	var err error
	if ok {
		message, err = h.service.GetWithOwnAssociations(ctx, id, requester)
		if err != nil {
			if errors.Is(err, core.ErrorNotFound{}) {
				return c.JSON(http.StatusNotFound, echo.Map{"error": "Message not found"})
			}
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}
	} else {
		message, err = h.service.Get(ctx, id, "")
		if err != nil {
			if errors.Is(err, core.ErrorNotFound{}) {
				return c.JSON(http.StatusNotFound, echo.Map{"error": "Message not found"})
			}
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}
	}
	return c.JSON(http.StatusOK, echo.Map{
		"status":  "ok",
		"content": message,
	})
}
