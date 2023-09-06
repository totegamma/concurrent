// Package userkv provides a simple key-value store for users.
package userkv

import (
	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel"
	"io/ioutil"
	"net/http"
)

var tracer = otel.Tracer("userkv")

// Handler is the interface for handling HTTP requests
type Handler interface {
    Get(c echo.Context) error
    Upsert(c echo.Context) error
}

type handler struct {
	service       Service
	entityService entity.Service
}

// NewHandler creates a new handler
func NewHandler(service Service, entityService entity.Service) Handler {
	return &handler{service, entityService}
}

// Get returns a userkv by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()

	claims := c.Get("jwtclaims").(util.JwtClaims)
	userID := claims.Audience
	if h.entityService.IsUserExists(ctx, userID) == false {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "user not found"})
	}
	key := c.Param("key")
	value, err := h.service.Get(ctx, userID, key)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": value})
}

// Upsert updates a userkv
func (h handler) Upsert(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpsert")
	defer span.End()

	claims := c.Get("jwtclaims").(util.JwtClaims)
	userID := claims.Audience
	if h.entityService.IsUserExists(ctx, userID) == false {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "user not found"})
	}
	key := c.Param("key")
	body := c.Request().Body
	bytes, err := ioutil.ReadAll(body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	value := string(bytes)

	err = h.service.Upsert(ctx, userID, key, value)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}
