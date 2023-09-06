// Package character is handling concurrent Character object
package character

import (
	"errors"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"net/http"
)

var tracer = otel.Tracer("character")

// Handler is the interface for handling HTTP requests
type Handler interface {
    Get(c echo.Context) error
    Put(c echo.Context) error
}

type handler struct {
	service Service
}

// NewHandler creates a new handler
func NewHandler(service Service) Handler {
	return &handler{service: service}
}

// Get returns a character by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()

	author := c.QueryParam("author")
	schema := c.QueryParam("schema")
	characters, err := h.service.GetCharacters(ctx, author, schema)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Character not found"})
		}
		return err
	}
	response := CharactersResponse{
		Characters: characters,
	}
	return c.JSON(http.StatusOK, response)
}

// Put updates a character
func (h handler) Put(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerPut")
	defer span.End()

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
