// Package userkv provides a simple key-value store for users.
package userkv

import (
    "io/ioutil"
    "net/http"
    "github.com/labstack/echo/v4"
    "github.com/totegamma/concurrent/x/util"
    "github.com/totegamma/concurrent/x/entity"
    "go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("userkv")

// Handler is the userkv handler
type Handler struct {
    service *Service
    entityService *entity.Service
}

// NewHandler is for wire.go
func NewHandler(service *Service, entityService *entity.Service) *Handler {
    return &Handler{service, entityService}
}

// Get returns a userkv by ID
func (h Handler) Get(c echo.Context) error {
    ctx, childSpan := tracer.Start(c.Request().Context(), "HandlerGet")
    defer childSpan.End()

    claims := c.Get("jwtclaims").(util.JwtClaims)
    userID := claims.Audience
    if (h.entityService.IsUserExists(ctx, userID) == false) {
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
func (h Handler) Upsert(c echo.Context) error {
    ctx, childSpan := tracer.Start(c.Request().Context(), "HandlerUpsert")
    defer childSpan.End()

    claims := c.Get("jwtclaims").(util.JwtClaims)
    userID := claims.Audience
    if (h.entityService.IsUserExists(ctx, userID) == false) {
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


