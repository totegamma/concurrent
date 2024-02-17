// Package message is handles concurrent message objects
package message

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/auth"
	"github.com/totegamma/concurrent/x/core"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("message")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	Post(c echo.Context) error
	Delete(c echo.Context) error
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
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()

	id := c.Param("id")

	requester, ok := c.Get(auth.RequesterIdCtxKey).(string)
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
		message, err = h.service.Get(ctx, id)
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

// Post creates a new message
// returns the created message
func (h handler) Post(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerPost")
	defer span.End()

	var request postRequest
	err := c.Bind(&request)
	if err != nil {
		return err
	}
	message, err := h.service.PostMessage(ctx, request.SignedObject, request.Signature, request.Streams)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": message})
}

// Delete deletes a message
// returns the deleted message
func (h handler) Delete(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDelete")
	defer span.End()

	messageID := c.Param("id")

	target, err := h.service.Get(ctx, messageID)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "target message not found"})
	}

	requester, ok := c.Get(auth.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}
	if target.Author != requester {
		return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action"})
	}

	deleted, err := h.service.Delete(ctx, messageID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": deleted})
}
