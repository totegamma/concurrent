// Package character is handling concurrent Character object
package character

import (
    "errors"
    "net/http"
    "gorm.io/gorm"
    "github.com/labstack/echo/v4"
    "go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("character")

// Handler is handles Character Object
type Handler struct {
    service *Service
}

// NewHandler is for wire
func NewHandler(service *Service) *Handler {
    return &Handler{service: service}
}

// Get is for Handling HTTP Get Method
func (h Handler) Get(c echo.Context) error {
    ctx, childSpan := tracer.Start(c.Request().Context(), "HandlerGet")
    defer childSpan.End()

    author := c.QueryParam("author")
    schema := c.QueryParam("schema")
    characters, err := h.service.GetCharacters(ctx, author, schema)
    if err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return c.JSON(http.StatusNotFound, echo.Map{"error": "Character not found"})
        }
        return err
    }
    response := CharactersResponse {
        Characters: characters,
    }
    return c.JSON(http.StatusOK, response)
}

// Put is for Handling HTTP Put Method
func (h Handler) Put(c echo.Context) error {
    ctx, childSpan := tracer.Start(c.Request().Context(), "HandlerPut")
    defer childSpan.End()

    var request postRequest
    err := c.Bind(&request)
    if err != nil {
        return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request", "message": err.Error()})
    }

    updated, err := h.service.PutCharacter(ctx, request.SignedObject, request.Signature, request.ID)
    if err != nil {
        return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request", "message": err.Error()})
    }
    return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": updated})
}

