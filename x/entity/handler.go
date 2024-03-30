// Package entity is handles concurrent message objects
package entity

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("entity")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	List(c echo.Context) error
	Update(c echo.Context) error
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
	extension := c.QueryParam("extension")
	entity, err := h.service.GetWithExtension(ctx, id, extension)
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

	since, err := strconv.ParseInt(c.QueryParam("since"), 10, 64)
	if err != nil {
		entities, err := h.service.List(ctx)
		if err != nil {
			span.RecordError(err)
			return err
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entities})
	} else {
		entities, err := h.service.ListModified(ctx, time.Unix(since, 0))
		if err != nil {
			span.RecordError(err)
			return err
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entities})
	}
}

// Update updates an entity
func (h handler) Update(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpdate")
	defer span.End()

	var request core.Entity
	err := c.Bind(&request)
	if err != nil {
		span.RecordError(err)
		return err
	}
	err = h.service.Update(ctx, &request)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": request})
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
	fqdn, err := h.service.ResolveHost(ctx, id, hint)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": fqdn})
}
