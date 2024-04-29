// Package auth handles sever-side and client-side authentication
package auth

import (
	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"go.opentelemetry.io/otel"
	"net/http"
)

var tracer = otel.Tracer("auth")

// Handler is the interface for handling HTTP requests
type Handler interface {
	GetPassport(c echo.Context) error
}

type handler struct {
	service core.AuthService
}

// NewHandler creates a new handler
func NewHandler(service core.AuthService) Handler {
	return &handler{service}
}

// Claim is used for get server signed jwt
// input user signed jwt
func (h *handler) GetPassport(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Auth.Handler.GetPassport")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	keys, ok := ctx.Value(core.RequesterKeychainKey).([]core.Key)

	response, err := h.service.IssuePassport(ctx, requester, keys)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"content": response})
}
