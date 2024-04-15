// Package entity is handles concurrent message objects
package entity

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("entity")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	List(c echo.Context) error
	Delete(c echo.Context) error
	Resolve(c echo.Context) error
}

type handler struct {
	service Service
	config  util.Config
}

// NewHandler creates a new handler
func NewHandler(service Service, config util.Config) Handler {
	return &handler{service: service, config: config}
}

// Get returns an entity by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()

	id := c.Param("id")
	entity, err := h.service.Get(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			span.RecordError(err)
			return c.JSON(http.StatusNotFound, echo.Map{"error": "entity not found"})
		}
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entity})
}

// List returns a list of entities
func (h handler) List(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerList")
	defer span.End()

	entities, err := h.service.List(ctx)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entities})
}

// Delete deletes an entity
func (h handler) Delete(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDelete")
	defer span.End()

	id := c.Param("id")
	err := h.service.Delete(ctx, id)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}

// Resolve returns entity domain affiliation
func (h handler) Resolve(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerResolve")
	defer span.End()

	id := c.Param("id")
	hint := c.QueryParam("hint")
	entity, err := h.service.GetWithHint(ctx, id, hint)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entity.Domain})
}
