// Package entity is handles concurrent message objects
package entity

import (
    "errors"
    "net/http"
    "gorm.io/gorm"
    "github.com/labstack/echo/v4"
    "go.opentelemetry.io/otel"
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
    publicInfo := SafeEntity {
        ID: entity.ID,
        Role: entity.Role,
        Host: entity.Host,
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

    entities, err := h.service.List(ctx, )
    if err != nil {
        return err
    }
    return c.JSON(http.StatusOK, entities)
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
