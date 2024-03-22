// Package profile is handling concurrent Profile object
package profile

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/x/core"
)

var tracer = otel.Tracer("profile")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	Query(c echo.Context) error
	Put(c echo.Context) error
	Delete(c echo.Context) error
}

type handler struct {
	service Service
}

// NewHandler creates a new handler
func NewHandler(service Service) Handler {
	return &handler{service: service}
}

// Get returns a profile by id
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()

	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request", "message": "id is required"})
	}

	profile, err := h.service.Get(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Profile not found"})
		}
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": profile})
}

// Query returns a profile by author and schema
func (h handler) Query(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerQuery")
	defer span.End()

	author := c.QueryParam("author")
	schema := c.QueryParam("schema")

	var err error
	var profiles []core.Profile

	if author != "" && schema != "" {
		profiles, err = h.service.GetByAuthorAndSchema(ctx, author, schema)
	} else if author != "" {
		profiles, err = h.service.GetByAuthor(ctx, author)
	} else if schema != "" {
		profiles, err = h.service.GetBySchema(ctx, schema)
	} else {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request", "message": "author or schema is required"})
	}

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Profile not found"})
		}
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": profiles})
}

// Put updates a profile
func (h handler) Put(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerPut")
	defer span.End()

	var request postRequest
	err := c.Bind(&request)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request", "message": err.Error()})
	}

	updated, err := h.service.Put(ctx, request.SignedObject, request.Signature, request.ID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request", "message": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": updated})
}

func (h handler) Delete(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDelete")
	defer span.End()

	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request", "message": "id is required"})
	}

	requester, ok := c.Get(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	target, err := h.service.Get(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "Profile not found"})
	}

	if target.Author != requester {
		return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action"})
	}

	deleted, err := h.service.Delete(ctx, id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": deleted})
}
