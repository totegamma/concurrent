// Package entity is handles concurrent message objects
package entity

import (
	"fmt"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"github.com/xinguang/go-recaptcha"
)

var tracer = otel.Tracer("handler")

// Handler handles Message objects
type Handler struct {
	service *Service
	rdb     *redis.Client
	config  util.Config
}

// NewHandler is for wire.go
func NewHandler(service *Service, rdb *redis.Client, config util.Config) *Handler {
	return &Handler{service: service, rdb: rdb, config: config}
}

// Get is for Handling HTTP Get Method
// Input: path parameter "id"
// Output: Message Object
func (h Handler) Get(c echo.Context) error {
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
	publicInfo := SafeEntity{
		ID:     entity.ID,
		Tag:    entity.Tag,
		Domain: entity.Domain,
		Certs:  entity.Certs,
		CDate:  entity.CDate,
	}
	return c.JSON(http.StatusOK, publicInfo)
}

// Post is for Handling HTTP Post Method
// Input: postRequset object
// Output: nothing
// Effect: register message object to database
func (h Handler) Register(c echo.Context) error {
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
		_, err = h.rdb.Get(ctx, "jti:" + claims.JWTID).Result()
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
		err = h.rdb.Set(ctx, "jti:" + jwtID, "1", expiration).Err()
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}
	}

	return c.String(http.StatusCreated, "{\"message\": \"accept\"}")
}

func (h Handler) Create(c echo.Context) error {
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

// List returns all known entity list
func (h Handler) List(c echo.Context) error {
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

// Update updates entity information
func (h Handler) Update(c echo.Context) error {
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

// Delete is for Handling HTTP Delete Method
func (h Handler) Delete(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDelete")
	defer span.End()

	id := c.Param("id")
	err := h.service.Delete(ctx, id)
	if err != nil {
		return err
	}
	return c.String(http.StatusOK, "{\"message\": \"accept\"}")
}
