// Package entity is handles concurrent message objects
package entity

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("handler")

// Handler handles Message objects
type Handler struct {
	service *Service
}

// NewHandler is for wire.go
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Get is for Handling HTTP Get Method
// Input: path parameter "id"
// Output: Message Object
func (h Handler) Get(c echo.Context) error {
	ctx, childSpan := tracer.Start(c.Request().Context(), "HandlerGet")
	defer childSpan.End()

	id := c.Param("id")
	entity, err := h.service.Get(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "entity not found"})
		}
		return err
	}
	publicInfo := SafeEntity{
		ID:    entity.ID,
		Role:  entity.Role,
		Host:  entity.Host,
		Certs: entity.Certs,
		CDate: entity.CDate,
	}
	return c.JSON(http.StatusOK, publicInfo)
}

// Post is for Handling HTTP Post Method
// Input: postRequset object
// Output: nothing
// Effect: register message object to database
func (h Handler) Post(c echo.Context) error {
	ctx, childSpan := tracer.Start(c.Request().Context(), "HandlerPost")
	defer childSpan.End()

	var request postRequest
	err := c.Bind(&request)
	if err != nil {
		return err
	}
	err = h.service.Create(ctx, request.CCAddr, request.Meta)
	if err != nil {
		return err
	}
	return c.String(http.StatusCreated, "{\"message\": \"accept\"}")
}

// List returns all known entity list
func (h Handler) List(c echo.Context) error {
	ctx, childSpan := tracer.Start(c.Request().Context(), "HandlerList")
	defer childSpan.End()

	since, err := strconv.ParseInt(c.QueryParam("since"), 10, 64)
	if err != nil {
		entities, err := h.service.List(ctx)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, entities)
	} else {
		entities, err := h.service.ListModified(ctx, time.Unix(since, 0))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, entities)
	}
}

// Update updates entity information
func (h Handler) Update(c echo.Context) error {
	ctx, childSpan := tracer.Start(c.Request().Context(), "HandlerUpdate")
	defer childSpan.End()

	var request core.Entity
	err := c.Bind(&request)
	if err != nil {
		return err
	}
	err = h.service.Update(ctx, &request)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": request})
}

// Delete is for Handling HTTP Delete Method
func (h Handler) Delete(c echo.Context) error {
	ctx, childSpan := tracer.Start(c.Request().Context(), "HandlerDelete")
	defer childSpan.End()

	id := c.Param("id")
	err := h.service.Delete(ctx, id)
	if err != nil {
		return err
	}
	return c.String(http.StatusOK, "{\"message\": \"accept\"}")
}
