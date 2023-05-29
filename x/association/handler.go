// Package association is handles concurrent Association objects
package association

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/util"
	"gorm.io/gorm"
)

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
    id := c.Param("id")

    association, err := h.service.Get(id)
    if err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return c.JSON(http.StatusNotFound, echo.Map{"error": "association not found"})
        }
        return err
    }
    response := associationResponse {
        Association: association,
    }
    return c.JSON(http.StatusOK, response)
}

// Post is for Handling HTTP Post Method
func (h Handler) Post(c echo.Context) error {
    var request postRequest
    err := c.Bind(&request)
    if err != nil {
        return err
    }
    err = h.service.PostAssociation(request.SignedObject, request.Signature, request.Streams, request.TargetType)
    if err != nil {
        return err
    }
    return c.String(http.StatusCreated, "{\"message\": \"accept\"}")
}

// Delete is for Handling HTTP Delete Method
func (h Handler) Delete(c echo.Context) error {
    var request deleteQuery
    err := c.Bind(&request)
    if (err != nil) {
        return err
    }

    target, err := h.service.Get(request.ID)
    if err != nil {
        return c.JSON(http.StatusNotFound, echo.Map{"error": "target association not found"})
    }

    claims := c.Get("jwtclaims").(util.JwtClaims)
    requester := claims.Audience
    if target.Author != requester {
        return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action"})
    }

    err = h.service.Delete(request.ID)
    if err != nil {
        return err
    }
    return c.String(http.StatusOK, "{\"message\": \"accept\"}")
}

