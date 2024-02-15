// Package auth handles sever-side and client-side authentication
package auth

import (
	"encoding/json"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"net/http"

	"github.com/totegamma/concurrent/x/core"
)

var tracer = otel.Tracer("auth")

// Handler is the interface for handling HTTP requests
type Handler interface {
	GetPassport(c echo.Context) error
	GetKeyResolution(c echo.Context) error
	UpdateKey(c echo.Context) error
}

type handler struct {
	service Service
}

// NewHandler creates a new handler
func NewHandler(service Service) Handler {
	return &handler{service}
}

// Claim is used for get server signed jwt
// input user signed jwt
func (h *handler) GetPassport(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetPassport")
	defer span.End()

	remote := c.Param("remote")
	requester := c.Get("requester").(string)

	response, err := h.service.IssuePassport(ctx, requester, remote)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"content": response})
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
