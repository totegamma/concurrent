// Package message is handles concurrent message objects
package message

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("message")

// Handler handles Message objects
type Handler struct {
	service *Service
}

// NewHandler is for wire.go
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Get is for Handling HTTP Get Method
// Input: path parameter "id"
// Output: Message Object
func (h Handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()

	id := c.Param("id")

	message, err := h.service.Get(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Message not found"})
		}
		return err
	}
	return c.JSON(http.StatusOK, message)
}

// Post is for Handling HTTP Post Method
// Input: Message Object
// Output: nothing
// Effect: register message object to database
func (h Handler) Post(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerPost")
	defer span.End()

	var request postRequest
	err := c.Bind(&request)
	if err != nil {
		return err
	}
	message, err := h.service.PostMessage(ctx, request.SignedObject, request.Signature, request.Streams)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": message})
}

// Delete deletes message. only auther can perform this.
func (h Handler) Delete(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDelete")
	defer span.End()

	var request deleteQuery
	err := c.Bind(&request)
	if err != nil {
		return err
	}

	target, err := h.service.Get(ctx, request.ID)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "target message not found"})
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
