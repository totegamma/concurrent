package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/xid"
	"golang.org/x/exp/slices"
	"gorm.io/gorm"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
)

var tracer = otel.Tracer("host")

// Handler is handles websocket
type Handler struct {
	service *Service
	config  util.Config
}

// NewHandler is used for wire.go
func NewHandler(service *Service, config util.Config) *Handler {
	return &Handler{service, config}
}

// Get returns a host by ID
func (h Handler) Get(c echo.Context) error {
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
	return c.JSON(http.StatusOK, host)

}

// Upsert updates Domain registry
func (h Handler) Upsert(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpsert")
	defer span.End()

	var host core.Domain
	err := c.Bind(&host)
	if err != nil {
		return err
	}
	err = h.service.Upsert(ctx, &host)
	if err != nil {
		return err
	}
	return c.String(http.StatusCreated, "{\"message\": \"accept\"}")
}

// List returns all hosts
func (h Handler) List(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerList")
	defer span.End()

	hosts, err := h.service.List(ctx)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, hosts)
}

// Profile returns server Profile
func (h Handler) Profile(c echo.Context) error {
	_, span := tracer.Start(c.Request().Context(), "HandlerProfile")
	defer span.End()

	return c.JSON(http.StatusOK, Profile{
		ID:     h.config.Concurrent.FQDN,
		CCID:   h.config.Concurrent.CCID,
		Pubkey: h.config.Concurrent.Pubkey,
	})
}

// Hello iniciates a new host registration
func (h Handler) Hello(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerHello")
	defer span.End()

	var newcomer Profile
	err := c.Bind(&newcomer)
	if err != nil {
		return err
	}

	// challenge
	req, err := http.NewRequest("GET", "https://"+newcomer.ID+"/api/v1/host", nil)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusBadRequest, err.Error())
	}
	// Inject the current span context into the request
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := new(http.Client)
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusBadRequest, err.Error())
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	var fetchedProf Profile
	json.Unmarshal(body, &fetchedProf)

	if newcomer.ID != fetchedProf.ID {
		return c.String(http.StatusBadRequest, "validation failed")
	}

	h.service.Upsert(ctx, &core.Domain{
		ID:     newcomer.ID,
		CCID:   newcomer.CCID,
		Role:   "unassigned",
		Pubkey: newcomer.Pubkey,
	})

	return c.JSON(http.StatusOK, Profile{
		ID:     h.config.Concurrent.FQDN,
		CCID:   h.config.Concurrent.CCID,
		Pubkey: h.config.Concurrent.Pubkey,
	})
}

// SayHello iniciates a new host registration
// Only Admin can call this
func (h Handler) SayHello(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerSayHello")
	defer span.End()

	target := c.Param("fqdn")

	claims := c.Get("jwtclaims").(util.JwtClaims)
	if !slices.Contains(h.config.Concurrent.Admins, claims.Audience) {
		return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action"})
	}

	me := Profile{
		ID:     h.config.Concurrent.FQDN,
		CCID:   h.config.Concurrent.CCID,
		Pubkey: h.config.Concurrent.Pubkey,
	}

	meStr, err := json.Marshal(me)

	// challenge
	jwt, err := util.CreateJWT(util.JwtClaims{
		Issuer:         h.config.Concurrent.CCID,
		Subject:        "CONCURRENT_API",
		Audience:       target,
		ExpirationTime: strconv.FormatInt(time.Now().Add(1*time.Minute).Unix(), 10),
		NotBefore:      strconv.FormatInt(time.Now().Unix(), 10),
		IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
		JWTID:          xid.New().String(),
	}, h.config.Concurrent.Prvkey)

	req, err := http.NewRequest("POST", "https://"+target+"/api/v1/domains/hello", bytes.NewBuffer(meStr))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusBadRequest, err.Error())
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	req.Header.Add("content-type", "application/json")
	req.Header.Add("authorization", "Bearer "+jwt)
	client := new(http.Client)
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusBadRequest, err.Error())
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	var fetchedProf Profile
	json.Unmarshal(body, &fetchedProf)

	if target != fetchedProf.ID {
		span.SetStatus(codes.Error, fmt.Sprintf("target does not match fetched profile: %v", fetchedProf.ID))
		return c.String(http.StatusBadRequest, "validation failed")
	}

	h.service.Upsert(ctx, &core.Domain{
		ID:     fetchedProf.ID,
		CCID:   fetchedProf.CCID,
		Role:   "unassigned",
		Pubkey: fetchedProf.Pubkey,
	})

	return c.JSON(http.StatusOK, fetchedProf)
}

// Delete removes a host from the registry
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

// Update updates a host in the registry
func (h Handler) Update(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpdate")
	defer span.End()

	var host core.Domain
	err := c.Bind(&host)
	if err != nil {
		return err
	}
	err = h.service.Update(ctx, &host)
	if err != nil {
		return err
	}
	return c.String(http.StatusOK, "{\"message\": \"accept\"}")
}
