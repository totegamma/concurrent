// Package association is handles concurrent Association objects
package association

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("association")

// Handler handles Association objects
type Handler struct {
	service *Service
}

// NewHandler is for wire.go
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Get is for Handling HTTP Get Method
func (h Handler) Get(c echo.Context) error {
	ctx, childSpan := tracer.Start(c.Request().Context(), "HandlerGet")
	defer childSpan.End()
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
	ctx, childSpan := tracer.Start(c.Request().Context(), "HandlerPost")
	defer childSpan.End()

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
	ctx, childSpan := tracer.Start(c.Request().Context(), "HandlerDelete")
	defer childSpan.End()

	var request deleteQuery
	err := c.Bind(&request)
	if err != nil {
		return err
	}

	target, err := h.service.Get(ctx, request.ID)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "target association not found"})
	}

	claims := c.Get("jwtclaims").(util.JwtClaims)
	requester := claims.Audience
	if target.Author != requester {
		return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action"})
	}

	deleted, err := h.service.Delete(ctx, request.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": deleted})
}
