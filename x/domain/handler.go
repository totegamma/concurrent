package domain

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
)

var tracer = otel.Tracer("domain")

// Service is the domain service interface
type Handler interface {
	Get(c echo.Context) error
	Upsert(c echo.Context) error
	List(c echo.Context) error
	Profile(c echo.Context) error
	Hello(c echo.Context) error
	SayHello(c echo.Context) error
	Delete(c echo.Context) error
	Update(c echo.Context) error
}

type handler struct {
	service Service
	config  util.Config
}

// NewHandler creates a new handler
func NewHandler(service Service, config util.Config) Handler {
	return &handler{service, config}
}

// Get returns a host by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()

	id := c.Param("id")
	host, err := h.service.GetByFQDN(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Domain not found"})
		}
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": host})

}

// Upsert creates or updates a host
func (h handler) Upsert(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpsert")
	defer span.End()

	var host core.Domain
	err := c.Bind(&host)
	if err != nil {
		return err
	}
	updated, err := h.service.Upsert(ctx, host)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": updated})
}

// List returns all hosts
func (h handler) List(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerList")
	defer span.End()

	hosts, err := h.service.List(ctx)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": hosts})
}

// Profile returns the host profile
func (h handler) Profile(c echo.Context) error {
	_, span := tracer.Start(c.Request().Context(), "HandlerProfile")
	defer span.End()

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": Profile{
		ID:     h.config.Concurrent.FQDN,
		CCID:   h.config.Concurrent.CCID,
		Pubkey: h.config.Concurrent.PublicKey,
	}})
}

// Hello creates a challenge response for another host
// If the challenge is accepted, the host will be added to the database
func (h handler) Hello(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerHello")
	defer span.End()

	var newcomer Profile
	err := c.Bind(&newcomer)
	if err != nil {
		return err
	}

	profile, err := h.service.Hello(ctx, newcomer)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": profile})
}

// SayHello initiates a challenge to a remote host
// The remote host will respond with a signed JWT
// If the JWT is valid, the remote host will be added to the database
func (h handler) SayHello(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerSayHello")
	defer span.End()

	target := c.Param("id")

	fetched, err := h.service.SayHello(ctx, target)

	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": fetched})
}

// Delete removes a host from the registry
func (h handler) Delete(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDelete")
	defer span.End()

	id := c.Param("id")
	err := h.service.Delete(ctx, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": id})
}

// Update updates a host in the registry
func (h handler) Update(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpdate")
	defer span.End()

	var host core.Domain
	err := c.Bind(&host)
	if err != nil {
		return err
	}
	err = h.service.Update(ctx, host)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": host})
}
