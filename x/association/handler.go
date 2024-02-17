// Package association is handles concurrent Association objects
package association

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/auth"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("association")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	Post(c echo.Context) error
	Delete(c echo.Context) error
	GetFiltered(c echo.Context) error
	GetCounts(c echo.Context) error
	GetOwnByTarget(c echo.Context) error
}

type handler struct {
	service Service
}

// NewHandler creates a new handler
func NewHandler(service Service) Handler {
	return &handler{service: service}
}

// Get returns an association by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()
	id := c.Param("id")

	association, err := h.service.Get(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "association not found"})
		}
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": association})
}

func (h handler) GetOwnByTarget(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetOwnByTarget")
	defer span.End()

	targetID := c.Param("id")

	requester, _ := c.Get(auth.RequesterIdCtxKey).(string)

	associations, err := h.service.GetOwnByTarget(ctx, targetID, requester)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": associations})
}

func (h handler) GetCounts(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetCounts")
	defer span.End()

	messageID := c.Param("id")
	schema := c.QueryParam("schema")
	if schema == "" {
		counts, err := h.service.GetCountsBySchema(ctx, messageID)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": counts})
	} else {
		counts, err := h.service.GetCountsBySchemaAndVariant(ctx, messageID, schema)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": counts})
	}
}

func (h handler) GetFiltered(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetFiltered")
	defer span.End()

	messageID := c.Param("id")
	schema := c.QueryParam("schema")
	variant := c.QueryParam("variant")

	if schema == "" {
		associations, err := h.service.GetByTarget(ctx, messageID)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": associations})
	} else if variant == "" {
		associations, err := h.service.GetBySchema(ctx, messageID, schema)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": associations})
	} else {
		associations, err := h.service.GetBySchemaAndVariant(ctx, messageID, schema, variant)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": associations})
	}
}

// Post creates a new association
// returns the created association
func (h handler) Post(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerPost")
	defer span.End()

	var request postRequest
	err := c.Bind(&request)
	if err != nil {
		return err
	}
	created, err := h.service.PostAssociation(ctx, request.SignedObject, request.Signature, request.Streams, request.TargetType)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, echo.Map{"status": "ok", "content": created})
}

// Delete deletes an association by ID
// returns the deleted association
func (h handler) Delete(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDelete")
	defer span.End()

	associationID := c.Param("id")
	requester, _ := c.Get(auth.RequesterIdCtxKey).(string)

	deleted, err := h.service.Delete(ctx, associationID, requester)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": deleted})
}
