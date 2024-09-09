// Package association is handles concurrent Association objects
package association

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/core"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("association")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	GetFiltered(c echo.Context) error
	GetCounts(c echo.Context) error
	GetOwnByTarget(c echo.Context) error
	GetAttached(c echo.Context) error
}

type handler struct {
	service core.AssociationService
}

// NewHandler creates a new handler
func NewHandler(service core.AssociationService) Handler {
	return &handler{service: service}
}

// Get returns an association by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Association.Handler.Get")
	defer span.End()
	id := c.Param("id")

	association, err := h.service.Get(ctx, id)
	if err != nil {
		span.RecordError(err)
		if errors.Is(err, core.ErrorNotFound{}) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "association not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": association})
}

func (h handler) GetOwnByTarget(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Association.Handler.GetOwnByTarget")
	defer span.End()

	targetID := c.Param("id")

	requester, _ := ctx.Value(core.RequesterIdCtxKey).(string)

	associations, err := h.service.GetOwnByTarget(ctx, targetID, requester)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": associations})
}

func (h handler) GetCounts(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Association.Handler.GetCounts")
	defer span.End()

	messageID := c.Param("id")
	schema := c.QueryParam("schema")
	if schema == "" {
		counts, err := h.service.GetCountsBySchema(ctx, messageID)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": counts})
	} else {
		counts, err := h.service.GetCountsBySchemaAndVariant(ctx, messageID, schema)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": counts})
	}
}

func (h handler) GetFiltered(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Association.Handler.GetFiltered")
	defer span.End()

	messageID := c.Param("id")
	schema := c.QueryParam("schema")
	variant := c.QueryParam("variant")

	if schema == "" {
		associations, err := h.service.GetByTarget(ctx, messageID)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": associations})
	} else if variant == "" {
		associations, err := h.service.GetBySchema(ctx, messageID, schema)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": associations})
	} else {
		associations, err := h.service.GetBySchemaAndVariant(ctx, messageID, schema, variant)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": associations})
	}
}

func (h handler) GetAttached(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Association.Handler.GetAttached")
	defer span.End()

	messageID := c.Param("id")
	associations, err := h.service.GetByTarget(ctx, messageID)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": associations})
}
