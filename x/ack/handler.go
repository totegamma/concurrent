package ack

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"

	"github.com/totegamma/concurrent/core"
)

var tracer = otel.Tracer("ack")

// Handler is the interface for handling HTTP requests
type Handler interface {
	GetAcker(c echo.Context) error
	GetAcking(c echo.Context) error
}

type handler struct {
	service core.AckService
}

// NewHandler creates a new handler
func NewHandler(service core.AckService) Handler {
	return &handler{service: service}
}

// GetAcking returns acking entities
func (h handler) GetAcking(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Ack.Handler.GetAcking")
	defer span.End()

	id := c.Param("id")
	acks, err := h.service.GetAcking(ctx, id)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": acks})
}

// GetAcker returns an acker
func (h handler) GetAcker(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Ack.Handler.GetAcker")
	defer span.End()

	id := c.Param("id")
	acks, err := h.service.GetAcker(ctx, id)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": acks})
}
