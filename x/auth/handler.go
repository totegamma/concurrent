package auth

import (
    "github.com/labstack/echo/v4"
    "github.com/totegamma/concurrent/x/core"
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
func Claim(jwt string) string {
    return nil
}

