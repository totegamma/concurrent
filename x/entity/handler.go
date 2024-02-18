// Package entity is handles concurrent message objects
package entity

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
	"github.com/xinguang/go-recaptcha"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("entity")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	Register(c echo.Context) error
	Create(c echo.Context) error
	List(c echo.Context) error
	Update(c echo.Context) error
	Delete(c echo.Context) error
	Resolve(c echo.Context) error
	UpdateRegistration(c echo.Context) error // NOTE: for migration. Remove later
}

type handler struct {
	service Service
	config  util.Config
}

// NewHandler creates a new handler
func NewHandler(service Service, config util.Config) Handler {
	return &handler{service: service, config: config}
}

// Get returns an entity by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()

	id := c.Param("id")
	entity, err := h.service.Get(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			span.RecordError(err)
			return c.JSON(http.StatusNotFound, echo.Map{"error": "entity not found"})
		}
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entity})
}

// Register creates a new entity
// only accepts when the server registration is open
// validate captcha if captcha secret is set
// returns the created entity
func (h handler) Register(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerRegister")
	defer span.End()

	var request registerRequest
	err := c.Bind(&request)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if h.config.Server.CaptchaSecret != "" {
		validator, err := recaptcha.NewWithSecert(h.config.Server.CaptchaSecret)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create recaptcha validator")
		}
		err = validator.Verify(request.Captcha)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("invalid captcha")
		}
	}

	err = h.service.Register(ctx, request.CCID, request.Registration, request.Signature, request.Info, request.Invitation)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, echo.Map{"status": "ok"})
}

// Create creates a new entity
func (h handler) Create(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerCreate")
	defer span.End()

	var request createRequest
	err := c.Bind(&request)
	if err != nil {
		span.RecordError(err)
		return err
	}
	err = h.service.Create(ctx, request.CCID, request.Registration, request.Signature, request.Info)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusCreated, echo.Map{"status": "ok"})
}

// List returns a list of entities
func (h handler) List(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerList")
	defer span.End()

	since, err := strconv.ParseInt(c.QueryParam("since"), 10, 64)
	if err != nil {
		entities, err := h.service.List(ctx)
		if err != nil {
			span.RecordError(err)
			return err
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entities})
	} else {
		entities, err := h.service.ListModified(ctx, time.Unix(since, 0))
		if err != nil {
			span.RecordError(err)
			return err
		}
		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entities})
	}
}

// Update updates an entity
func (h handler) Update(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpdate")
	defer span.End()

	var request core.Entity
	err := c.Bind(&request)
	if err != nil {
		span.RecordError(err)
		return err
	}
	err = h.service.Update(ctx, &request)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": request})
}

// Delete deletes an entity
func (h handler) Delete(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDelete")
	defer span.End()

	id := c.Param("id")
	err := h.service.Delete(ctx, id)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}

// Resolve returns entity domain affiliation
func (h handler) Resolve(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerResolve")
	defer span.End()

	id := c.Param("id")
	hint := c.QueryParam("hint")
	fqdn, err := h.service.ResolveHost(ctx, id, hint)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": fqdn})
}

// UpdateRegistration updates an entity registration
func (h handler) UpdateRegistration(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpdateRegistration")
	defer span.End()

	var request createRequest
	err := c.Bind(&request)
	if err != nil {
		span.RecordError(err)
		return err
	}
	err = h.service.UpdateRegistration(ctx, request.CCID, request.Registration, request.Signature)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}
