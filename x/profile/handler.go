// Package profile is handling concurrent Profile object
package profile

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/core"
)

var tracer = otel.Tracer("profile")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	GetBySemanticID(c echo.Context) error
	Query(c echo.Context) error
}

type handler struct {
	service core.ProfileService
}

// NewHandler creates a new handler
func NewHandler(service core.ProfileService) Handler {
	return &handler{service: service}
}

// Get returns a profile by id
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Profile.Handler.Get")
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

func (h handler) GetBySemanticID(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Profile.Handler.GetBySemanticID")
	defer span.End()

	semanticID := c.Param("semanticid")
	owner := c.Param("owner")

	if semanticID == "" || owner == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request", "message": "semanticID and owner are required"})
	}

	profile, err := h.service.GetBySemanticID(ctx, semanticID, owner)
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
	ctx, span := tracer.Start(c.Request().Context(), "Profile.Handler.Query")
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
