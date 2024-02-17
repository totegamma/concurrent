package key

import (
	"encoding/json"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"net/http"

	"github.com/totegamma/concurrent/x/core"
)

var tracer = otel.Tracer("key")

// Handler is the interface for handling HTTP requests
type Handler interface {
	GetKeyResolution(c echo.Context) error
	GetKeyMine(c echo.Context) error
	UpdateKey(c echo.Context) error
}

type handler struct {
	service Service
}

// NewHandler creates a new handler
func NewHandler(service Service) Handler {
	return &handler{service}
}

// GetKeyResolution is used for get key resolution
func (h *handler) GetKeyResolution(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetKeyResolution")
	defer span.End()

	keyID := c.Param("id")

	response, err := h.service.GetKeyResolution(ctx, keyID)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"content": response})
}

type keyRequest struct {
	SignedObject string `json:"signedObject"`
	Signature    string `json:"signature"`
}

// UpdateKey is used for update key
func (h *handler) UpdateKey(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpdateKey")
	defer span.End()

	var request keyRequest
	err := c.Bind(&request)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}

	var object core.SignedObject[any]
	err = json.Unmarshal([]byte(request.SignedObject), &object)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}

	if object.Type == "enact" {
		created, err := h.service.EnactKey(ctx, request.SignedObject, request.Signature)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, echo.Map{"content": created})
	} else if object.Type == "revoke" {
		revoked, err := h.service.RevokeKey(ctx, request.SignedObject, request.Signature)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, echo.Map{"content": revoked})
	} else {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid type"})
	}
}

// GetKeyMine is used for get all keys of requester
func (h *handler) GetKeyMine(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetKeyMine")
	defer span.End()

	requester, ok := c.Get(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	response, err := h.service.GetAllKeys(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"content": response})
}


