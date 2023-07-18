// Package stream is for handling concurrent stream object
package stream

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("stream")

// Handler handles Stream objects
type Handler struct {
	service *Service
}

// NewHandler is for wire.go
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Get is for handling HTTP Get Method
func (h Handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()

	streamStr := c.QueryParam("stream")
	stream, err := h.service.Get(ctx, streamStr)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "User not found"})
		}
		return err
	}
	return c.JSON(http.StatusOK, stream)
}

// Put is for handling HTTP Put Method
func (h Handler) Put(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerPut")
	defer span.End()

	var request postRequest
	err := c.Bind(&request)
	if err != nil {
		return err
	}

	id, err := h.service.Upsert(ctx, request.SignedObject, request.Signature, request.ID)
	if err != nil {
		return err
	}
	return c.String(http.StatusCreated, fmt.Sprintf("{\"message\": \"accept\", \"id\": \"%s\"}", id))
}

// Recent returns recent messages in some streams
func (h Handler) Recent(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerRecent")
	defer span.End()

	streamsStr := c.QueryParam("streams")
	streams := strings.Split(streamsStr, ",")
	messages, _ := h.service.GetRecent(ctx, streams, 16)

	return c.JSON(http.StatusOK, messages)
}

// Range returns messages since to until in specified streams
func (h Handler) Range(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerRange")
	defer span.End()

	queryStreams := c.QueryParam("streams")
	streams := strings.Split(queryStreams, ",")
	querySince := c.QueryParam("since")
	queryUntil := c.QueryParam("until")

	since := "-"
	if querySince != "" {
		since = querySince
	}

	until := "+"
	if queryUntil != "" {
		until = queryUntil
	}

	messages, _ := h.service.GetRange(ctx, streams, since, until, 16)
	return c.JSON(http.StatusOK, messages)
}

// List returns stream ids which filtered by specific schema
func (h Handler) List(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerList")
	defer span.End()

	schema := c.QueryParam("schema")
	list, err := h.service.StreamListBySchema(ctx, schema)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, list)
}

// Checkpoint used by cross server communication
func (h Handler) Checkpoint(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerCheckpoint")
	defer span.End()

	var packet checkpointPacket
	err := c.Bind(&packet)
	if err != nil {
		span.RecordError(err)
		return err
	}

	err = h.service.Post(ctx, packet.Stream, packet.ID, packet.Type, packet.Author, packet.Host, packet.Owner)
	if err != nil {
		span.RecordError(err)
		return nil
	}

	return c.String(http.StatusCreated, fmt.Sprintf("{\"message\": \"accept\"}"))
}
