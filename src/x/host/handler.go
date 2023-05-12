package host

import (
    "net/http"
    "github.com/labstack/echo/v4"
)

// Handler is handles websocket
type Handler struct {
    service Service
}

// NewHandler is used for wire.go
func NewHandler(service Service) Handler {
    return Handler{service}
}

// Get returns a host by ID
func (h Handler) Get(c echo.Context) error {
    id := c.Param("id")
    host := h.service.Get(id)
    return c.JSON(http.StatusOK, host)

}

// Upsert updates Host registry
func (h Handler) Upsert(c echo.Context) error {
    var host Host
    err := c.Bind(&host)
    if (err != nil) {
        return err
    }
    h.service.Upsert(&host)
    return c.String(http.StatusCreated, "{\"message\": \"accept\"}")
}

// List returns all hosts
func (h Handler) List(c echo.Context) error {
    hosts := h.service.List()
    return c.JSON(http.StatusOK, hosts)
}

