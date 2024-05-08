// Package entity is handles concurrent message objects
package entity

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/core"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("entity")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	List(c echo.Context) error
}

type handler struct {
	service core.EntityService
}

// NewHandler creates a new handler
func NewHandler(service core.EntityService) Handler {
	return &handler{service: service}
}

// Get returns an entity by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Entity.Handler.Get")
	defer span.End()

	id := c.Param("id")
	hint := c.QueryParam("hint")
	var entity core.Entity
	var err error

	if strings.Contains(id, ".") {
		entity, err = h.service.GetByAlias(ctx, id)
		if err != nil {
			span.RecordError(err)
			return err
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entity})
	}

	if hint == "" {
		entity, err = h.service.Get(ctx, id)
	} else {
		entity, err = h.service.GetWithHint(ctx, id, hint)
	}
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
	ctx, span := tracer.Start(c.Request().Context(), "Entity.Handler.List")
	defer span.End()

	entities, err := h.service.List(ctx)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entities})
}
