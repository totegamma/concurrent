// Package userkv provides a simple key-value store for users.
package userkv

import (
	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"go.opentelemetry.io/otel"
	"io"
	"net/http"
)

var tracer = otel.Tracer("userkv")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	Upsert(c echo.Context) error
}

type handler struct {
	service Service
}

// NewHandler creates a new handler
func NewHandler(service Service) Handler {
	return &handler{service}
}

// Get returns a userkv by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "UserKV.Handler.Get")
	defer span.End()

	requester, ok := c.Get(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	key := c.Param("key")
	value, err := h.service.Get(ctx, requester, key)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": value})
}

// Upsert updates a userkv
func (h handler) Upsert(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "UserKV.Handler.Upsert")
	defer span.End()

	requester, ok := c.Get(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	key := c.Param("key")
	body := c.Request().Body
	bytes, err := io.ReadAll(body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	value := string(bytes)

	err = h.service.Upsert(ctx, requester, key, value)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}
