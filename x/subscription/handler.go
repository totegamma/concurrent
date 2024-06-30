package subscription

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"

	"github.com/totegamma/concurrent/core"
)

var tracer = otel.Tracer("collection")

// Handler is the interface for handling HTTP requests
type Handler interface {
	GetSubscription(c echo.Context) error
	GetOwnSubscriptions(c echo.Context) error
}

type handler struct {
	service core.SubscriptionService
}

// NewHandler creates a new handler
func NewHandler(service core.SubscriptionService) Handler {
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
		if errors.Is(err, core.ErrorNotFound{}) {
			return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "subscription not found"})
		}
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": data})
}

// GetOwnSubscriptions
func (h *handler) GetOwnSubscriptions(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Subscription.Handler.GetOwnSubscriptions")
	defer span.End()

	requesterContext, ok := ctx.Value(core.RequesterContextCtxKey).(core.RequesterContext)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}
	requester := requesterContext.Entity.ID

	data, err := h.service.GetOwnSubscriptions(ctx, requester)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": data})
}
