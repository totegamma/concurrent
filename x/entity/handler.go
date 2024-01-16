// Package entity is handles concurrent message objects
package entity

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
	"github.com/xinguang/go-recaptcha"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("handler")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	Register(c echo.Context) error
	Create(c echo.Context) error
	List(c echo.Context) error
	Update(c echo.Context) error
	Delete(c echo.Context) error
	Ack(c echo.Context) error
	Unack(c echo.Context) error
	GetAcker(c echo.Context) error
	GetAcking(c echo.Context) error
}

type handler struct {
	service Service
	rdb     *redis.Client
	config  util.Config
}

// NewHandler creates a new handler
func NewHandler(service Service, rdb *redis.Client, config util.Config) Handler {
	return &handler{service: service, rdb: rdb, config: config}
}

// Get returns an entity by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()

	id := c.Param("id")
	entity, err := h.service.Get(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "entity not found"})
		}
		return err
	}

	return c.JSON(http.StatusOK, entity)
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

	inviter := ""
	jwtID := ""
	expireAt := int64(0)
	if request.Token != "" {
		claims, err := util.ValidateJWT(request.Token)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid token"})
		}
		if claims.Subject != "CONCURRENT_INVITE" {
			return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid token"})
		}
		_, err = h.rdb.Get(ctx, "jti:"+claims.JWTID).Result()
		if err == nil {
			span.RecordError(err)
			return c.JSON(http.StatusUnauthorized, echo.Map{"error": "token is already used"})
		}

		inviter = claims.Issuer
		jwtID = claims.JWTID
		expireAt, _ = strconv.ParseInt(claims.ExpirationTime, 10, 64)
	}

	err = h.service.Register(ctx, request.CCID, request.Meta, inviter)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}

	if jwtID != "" {
		expiration := time.Until(time.Unix(int64(expireAt), 0))
		err = h.rdb.Set(ctx, "jti:"+jwtID, "1", expiration).Err()
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}
	}

	return c.String(http.StatusCreated, "{\"message\": \"accept\"}")
}

// Create creates a new entity
func (h handler) Create(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerCreate")
	defer span.End()

	var request createRequest
	err := c.Bind(&request)
	if err != nil {
		return err
	}
	err = h.service.Create(ctx, request.CCID, request.Meta)
	if err != nil {
		return err
	}
	return c.String(http.StatusCreated, "{\"message\": \"accept\"}")
}

// List returns a list of entities
func (h handler) List(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerList")
	defer span.End()

	since, err := strconv.ParseInt(c.QueryParam("since"), 10, 64)
	if err != nil {
		entities, err := h.service.List(ctx)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, entities)
	} else {
		entities, err := h.service.ListModified(ctx, time.Unix(since, 0))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, entities)
	}
}

// Update updates an entity
func (h handler) Update(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpdate")
	defer span.End()

	var request core.Entity
	err := c.Bind(&request)
	if err != nil {
		return err
	}
	err = h.service.Update(ctx, &request)
	if err != nil {
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
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}

// Ack creates a new ack
func (h handler) Ack(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerAck")
	defer span.End()

	var request ackRequest
	err := c.Bind(&request)
	if err != nil {
		return err
	}

	err = h.service.Ack(ctx, request.SignedObject, request.Signature)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}

// Unack deletes an ack
func (h handler) Unack(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUnack")
	defer span.End()

	var request ackRequest
	err := c.Bind(&request)
	if err != nil {
		return err
	}

	err = h.service.Unack(ctx, request.SignedObject, request.Signature)
	if err != nil {
		return err
	}

	return c.String(http.StatusOK, "{\"message\": \"accept\"}")
}

// GetAcking returns acking entities
func (h handler) GetAcking(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetAcking")
	defer span.End()

	id := c.Param("id")
	acks, err := h.service.GetAcking(ctx, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": acks})
}

// GetAcker returns an acker
func (h handler) GetAcker(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetAcker")
	defer span.End()

	id := c.Param("id")
	acks, err := h.service.GetAcker(ctx, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": acks})
}


