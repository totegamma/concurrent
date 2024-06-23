package domain

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"

	"github.com/totegamma/concurrent/core"
)

var tracer = otel.Tracer("domain")

// Service is the domain service interface
type Handler interface {
	Get(c echo.Context) error
	List(c echo.Context) error
}

type handler struct {
	service core.DomainService
}

// NewHandler creates a new handler
func NewHandler(service core.DomainService) Handler {
	return &handler{service}
}

// Get returns a host by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Domain.Handler.Get")
	defer span.End()

	id := c.Param("id")
	host, err := h.service.GetByFQDN(ctx, id)
	if err != nil {
		if errors.Is(err, core.ErrorNotFound{}) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Domain not found"})
		}
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": host})

}

// List returns all hosts
func (h handler) List(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Domain.Handler.List")
	defer span.End()

	hosts, err := h.service.List(ctx)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": hosts})
}
