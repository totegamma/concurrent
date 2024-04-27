package subscription

import (
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"net/http"

	"github.com/totegamma/concurrent/x/core"
)

var tracer = otel.Tracer("collection")

// Handler is the interface for handling HTTP requests
type Handler interface {
	GetSubscription(c echo.Context) error
	GetOwnSubscriptions(c echo.Context) error
}

type handler struct {
	service Service
}

// NewHandler creates a new handler
func NewHandler(service Service) Handler {
	return &handler{
		service: service,
	}
}

// GetSubscription returns a collection by ID
func (h *handler) GetSubscription(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Subscription.Handler.GetSubscription")
	defer span.End()

	id := c.Param("id")

	data, err := h.service.GetSubscription(ctx, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": data})
}

// GetOwnSubscriptions
func (h *handler) GetOwnSubscriptions(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Subscription.Handler.GetOwnSubscriptions")
	defer span.End()

	requester, _ := ctx.Value(core.RequesterIdCtxKey).(string)

	data, err := h.service.GetOwnSubscriptions(ctx, requester)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": data})
}
