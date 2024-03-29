package ack

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("ack")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Ack(c echo.Context) error
	GetAcker(c echo.Context) error
	GetAcking(c echo.Context) error
}

type handler struct {
	service Service
}

// NewHandler creates a new handler
func NewHandler(service Service) Handler {
	return &handler{service: service}
}

// Ack creates a new ack
func (h handler) Ack(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerAck")
	defer span.End()

	var request ackRequest
	err := c.Bind(&request)
	if err != nil {
		span.RecordError(err)
		return err
	}

	err = h.service.Ack(ctx, request.SignedObject, request.Signature)
	if err != nil {
		span.RecordError(err)
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}

// GetAcking returns acking entities
func (h handler) GetAcking(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetAcking")
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
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetAcker")
	defer span.End()

	id := c.Param("id")
	acks, err := h.service.GetAcker(ctx, id)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": acks})
}
