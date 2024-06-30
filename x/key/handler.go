package key

import (
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"net/http"

	"github.com/totegamma/concurrent/core"
)

var tracer = otel.Tracer("key")

// Handler is the interface for handling HTTP requests
type Handler interface {
	GetKeyResolution(c echo.Context) error
	GetKeyMine(c echo.Context) error
}

type handler struct {
	service core.KeyService
}

// NewHandler creates a new handler
func NewHandler(service core.KeyService) Handler {
	return &handler{service}
}

// GetKeyResolution is used for get key resolution
func (h *handler) GetKeyResolution(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Key.Handler.GetKeyResolution")
	defer span.End()

	keyID := c.Param("id")

	response, err := h.service.GetKeyResolution(ctx, keyID)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"content": response})
}

// GetKeyMine is used for get all keys of requester
func (h *handler) GetKeyMine(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Key.Handler.GetKeyMine")
	defer span.End()

	requesterContext, ok := ctx.Value(core.RequesterContextCtxKey).(core.RequesterContext)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}
	requester := requesterContext.Entity.ID

	response, err := h.service.GetAllKeys(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"content": response})
}
