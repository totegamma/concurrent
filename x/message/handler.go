// Package message is handles concurrent message objects
package message

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("message")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
}

type handler struct {
	service Service
}

// NewHandler creates a new handler
func NewHandler(service Service) Handler {
	return &handler{service: service}
}

// Get returns an message by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Message.Handler.Get")
	defer span.End()

	id := c.Param("id")

	requester, ok := c.Get(core.RequesterIdCtxKey).(string)
	var message core.Message
	var err error
	if ok {
		message, err = h.service.GetWithOwnAssociations(ctx, id, requester)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return c.JSON(http.StatusNotFound, echo.Map{"error": "Message not found"})
			}
			return err
		}
	} else {
		message, err = h.service.Get(ctx, id, "")
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return c.JSON(http.StatusNotFound, echo.Map{"error": "Message not found"})
			}
			return err
		}
	}
	return c.JSON(http.StatusOK, echo.Map{
		"status":  "ok",
		"content": message,
	})
}
