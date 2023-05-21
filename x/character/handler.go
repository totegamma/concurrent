// Package character is handling concurrent Character object
package character

import (
    "net/http"
    "github.com/labstack/echo/v4"
)

// Handler is handles Character Object
type Handler struct {
    service Service
}

// NewHandler is for wire
func NewHandler(service Service) Handler {
    return Handler{service: service}
}

// Get is for Handling HTTP Get Method
func (h Handler) Get(c echo.Context) error {
    author := c.QueryParam("author")
    schema := c.QueryParam("schema")
    characters := h.service.GetCharacters(author, schema)
    response := CharactersResponse {
        Characters: characters,
    }
    return c.JSON(http.StatusOK, response)
}

// Put is for Handling HTTP Put Method
func (h Handler) Put(c echo.Context) error {

    var request postRequest
    err := c.Bind(&request)
    if err != nil {
        return err
    }

    err = h.service.PutCharacter(request.SignedObject, request.Signature, request.ID)
    if err != nil {
        return err
    }
    return c.String(http.StatusCreated, "{\"message\": \"accept\"}")
}

