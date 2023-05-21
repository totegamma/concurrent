// Package message is handles concurrent message objects
package message

import (
    "net/http"
    "github.com/labstack/echo/v4"
)

// Handler handles Message objects
type Handler struct {
    service Service
}

// NewHandler is for wire.go
func NewHandler(service Service) Handler {
    return Handler{service: service}
}

// Get is for Handling HTTP Get Method
// Input: path parameter "id"
// Output: Message Object
func (h Handler) Get(c echo.Context) error {
    id := c.Param("id")

    message := h.service.GetMessage(id)
    return c.JSON(http.StatusOK, message)
}

// Post is for Handling HTTP Post Method
// Input: Message Object
// Output: nothing
// Effect: register message object to database
func (h Handler) Post(c echo.Context) error {
    var request postRequest
    err := c.Bind(&request)
    if err != nil {
        return err
    }
    err = h.service.PostMessage(request.SignedObject, request.Signature, request.Streams)
    if err != nil {
        return err
    }
    return c.String(http.StatusCreated, "{\"message\": \"accept\"}")
}

