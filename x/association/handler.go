// Package association is handles concurrent Association objects
package association

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/util"
	"github.com/totegamma/concurrent/x/message"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("association")

// Handler handles Association objects
type Handler struct {
	service *Service
	message *message.Service
}

// NewHandler is for wire.go
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Get is for Handling HTTP Get Method
func (h Handler) Get(c echo.Context) error {
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
	response := associationResponse{
		Association: association,
	}
	return c.JSON(http.StatusOK, response)
}

// Post is for Handling HTTP Post Method
func (h Handler) Post(c echo.Context) error {
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

// Delete is for Handling HTTP Delete Method
func (h Handler) Delete(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDelete")
	defer span.End()

	associationID := c.Param("id")

	association, err := h.service.Get(ctx, associationID)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "target association not found"})
	}

	message, err := h.message.Get(ctx, association.TargetID)
	if err != nil {
		claims := c.Get("jwtclaims").(util.JwtClaims)
		requester := claims.Audience
		if (association.Author != requester) && (message.Author != requester) {
			return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action"})
		}
	}

	deleted, err := h.service.Delete(ctx, associationID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": deleted})
}
