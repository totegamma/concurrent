// Package auth handles sever-side and client-side authentication
package auth

import (
    "net/http"
    "github.com/labstack/echo/v4"
    "go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("auth")

// Handler is handles websocket
type Handler struct {
    service *Service
}

// NewHandler is used for wire.go
func NewHandler(service *Service) *Handler {
    return &Handler{service}
}

// Claim is used for get server signed jwt
// input user signed jwt
func (h *Handler) Claim(c echo.Context) error {
    ctx, span := tracer.Start(c.Request().Context(), "HandlerClaim")
    defer span.End()

    request := c.Request().Header.Get("authorization")
    if request == "" { // XXX for backward compatibility
        request = c.Request().Header.Get("Authentication")
    }

    response, err := h.service.IssueJWT(ctx, request)
    if err != nil {
        span.RecordError(err)
        return c.JSON(http.StatusUnauthorized, echo.Map{"error": err.Error()})
    }
    return c.JSON(http.StatusOK, echo.Map{"jwt": response})
}

