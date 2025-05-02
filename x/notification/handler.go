package notification

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"

	"github.com/concrnt/concrnt/core"
)

var tracer = otel.Tracer("notification")

type Handler interface {
	Subscribe(c echo.Context) error
	Delete(c echo.Context) error
	Get(c echo.Context) error
}

type handler struct {
	service core.NotificationService
}

func NewHandler(service core.NotificationService) Handler {
	return &handler{service: service}
}

// Subscribe creates or updates a notification subscription.
func (h *handler) Subscribe(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Notification.Handler.Subscribe")
	defer span.End()

	var subscription core.NotificationSubscription
	err := c.Bind(&subscription)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}

	subscription, err = h.service.Subscribe(ctx, subscription)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, echo.Map{"status": "ok", "content": subscription})
}

// Delete removes a notification subscription by vendor ID and owner.
func (h *handler) Delete(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Notification.Handler.Delete")
	defer span.End()

	owner := c.Param("owner")
	vendorID := c.Param("vendor_id")

	err := h.service.Delete(ctx, vendorID, owner)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}

// Get retrieves a notification subscription by vendor ID and owner.
func (h *handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Notification.Handler.Get")
	defer span.End()

	owner := c.Param("owner")
	vendorID := c.Param("vendor_id")

	subscription, err := h.service.Get(ctx, vendorID, owner)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": subscription})
}
