// Package profile is handling concurrent Profile object
package profile

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"

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
		if errors.Is(err, core.ErrorNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Profile not found"})
		}
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
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
		if errors.Is(err, core.ErrorNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Profile not found"})
		}
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": profile})
}

// Query returns a profile by author and schema
func (h handler) Query(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Profile.Handler.Query")
	defer span.End()

	author := c.QueryParam("author")
	schema := c.QueryParam("schema")
	limitStr := c.QueryParam("limit")
	sinceStr := c.QueryParam("since")
	untilStr := c.QueryParam("until")

	since := time.Time{}
	if sinceStr != "" {
		epoch, err := strconv.ParseInt(sinceStr, 10, 64)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
		}
		since = time.Unix(epoch, 0)
	}

	until := time.Time{}
	var err error
	if untilStr != "" {
		epoch, err := strconv.ParseInt(untilStr, 10, 64)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
		}
		until = time.Unix(epoch, 0)
	}

	limit := 16
	if limitStr != "" {
		limit, err = strconv.Atoi(limitStr)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
		}
	}

	if limit > 100 {
		limit = 100
	}

	profiles, err := h.service.Query(ctx, author, schema, limit, since, until)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": profiles})
}
