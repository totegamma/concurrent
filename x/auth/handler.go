// Package auth handles sever-side and client-side authentication
package auth

import (
    "fmt"
    "net/http"
    "github.com/labstack/echo/v4"
)

// Handler is handles websocket
type Handler struct {
    service *Service
}

// NewHandler is used for wire.go
func NewHandler(service *Service) *Handler {
    return &Handler{service}
}

// Claim is used for get server signed jwt
// input user signed jwt
func (h *Handler) Claim(c echo.Context) error {
    jwt := c.Request().Header.Get("Authentication")
    claim, err := h.service.ValidateJWT(jwt)
    if err != nil {
        return c.JSON(http.StatusUnauthorized, echo.Map{"error": err.Error()})
    }
    return c.JSON(http.StatusOK, echo.Map{"message": fmt.Sprintf("hello %v!", claim.Issuer)})
}

