package ack

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"

	"github.com/concrnt/concrnt/core"
)

var tracer = otel.Tracer("ack")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
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

// Get returns a ack
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Ack.Handler.Get")
	defer span.End()

	from := c.Param("from")
	to := c.Param("to")

	ack, err := h.service.Get(ctx, from, to)
	if err != nil {
		if err == core.ErrorNotFound {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Ack not found"})
		}
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": ack})
}

// GetAcking returns acking entities
func (h handler) GetAcking(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Ack.Handler.GetAcking")
	defer span.End()

	id := c.Param("id")
	acks, err := h.service.GetAcking(ctx, id)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
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
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": acks})
}
