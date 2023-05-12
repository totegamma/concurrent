// Package association is handles concurrent Association objects
package association

import (
    "net/http"
    "github.com/labstack/echo/v4"
)

// Handler handles Association objects
type Handler struct {
    service Service
}

// NewHandler is for wire.go
func NewHandler(service Service) Handler {
    return Handler{service: service}
}

// Get is for Handling HTTP Get Method
func (h Handler) Get(c echo.Context) error {
    id := c.Param("id")

    association := h.service.Get(id)
    response := associationResponse {
        Association: association,
    }
    return c.JSON(http.StatusOK, response)
}

// Post is for Handling HTTP Post Method
func (h Handler) Post(c echo.Context) error {

    var association Association
    err := c.Bind(&association)
    if (err != nil) {
        return err
    }

    h.service.PostAssociation(association)
    return c.String(http.StatusCreated, "{\"message\": \"accept\"}")
}

// Delete is for Handling HTTP Delete Method
func (h Handler) Delete(c echo.Context) error {

    var request deleteQuery
    err := c.Bind(&request)
    if (err != nil) {
        return err
    }

    h.service.Delete(request.ID)
    return c.String(http.StatusOK, "{\"message\": \"accept\"}")
}

