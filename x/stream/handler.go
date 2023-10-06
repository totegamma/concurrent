// Package stream is for handling concurrent stream object
package stream

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("stream")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	Create(c echo.Context) error
	Update(c echo.Context) error
	Recent(c echo.Context) error
	Range(c echo.Context) error
	List(c echo.Context) error
	ListMine(c echo.Context) error
	Delete(c echo.Context) error
	Remove(c echo.Context) error
	Checkpoint(c echo.Context) error
}

type handler struct {
	service Service
}

// NewHandler creates a new handler
func NewHandler(service Service) Handler {
	return &handler{service: service}
}

// Get returns a stream by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()

	streamID := c.Param("id")
	stream, err := h.service.GetStream(ctx, streamID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "User not found"})
		}
		return err
	}
	return c.JSON(http.StatusOK, stream)
}

// Create creates a new stream
func (h handler) Create(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerCreate")
	defer span.End()

	var data core.Stream
	err := c.Bind(&data)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
	}

	created, err := h.service.CreateStream(ctx, data)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
	}

	return c.JSON(http.StatusCreated, echo.Map{"status": "ok", "content": created})
}

// Update updates a stream
func (h handler) Update(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpdate")
	defer span.End()

	id := c.Param("id")

	var data core.Stream
	err := c.Bind(&data)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
	}

	data.ID = id

	updated, err := h.service.UpdateStream(ctx, data)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": updated})
}

// Recent returns recent messages in some streams
func (h handler) Recent(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerRecent")
	defer span.End()

	streamsStr := c.QueryParam("streams")
	streams := strings.Split(streamsStr, ",")
	messages, _ := h.service.GetRecentItems(ctx, streams, time.Now(), 16)

	return c.JSON(http.StatusOK, messages)
}

// Range returns messages since to until in specified streams
func (h handler) Range(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerRange")
	defer span.End()

	queryStreams := c.QueryParam("streams")
	streams := strings.Split(queryStreams, ",")
	querySince := c.QueryParam("since")
	queryUntil := c.QueryParam("until")

	if querySince != "" {
		sinceEpoch, err := strconv.ParseInt(querySince, 10, 64)
		if err != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
		}
		since := time.Unix(sinceEpoch, 0)
		messages, _ := h.service.GetImmediateItems(ctx, streams, since, 16)
		return c.JSON(http.StatusOK, messages)
	} else if queryUntil != "" {
		untilEpoch, err := strconv.ParseInt(queryUntil, 10, 64)
		if err != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
		}
		until := time.Unix(untilEpoch, 0)
		messages, _ := h.service.GetRecentItems(ctx, streams, until, 16)
		return c.JSON(http.StatusOK, messages)
	} else {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
	}
}

// List returns stream ids which filtered by specific schema
func (h handler) List(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerList")
	defer span.End()

	schema := c.QueryParam("schema")
	list, err := h.service.ListStreamBySchema(ctx, schema)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, list)
}

// ListMine returns stream ids which filtered by specific schema
func (h handler) ListMine(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerListMine")
	defer span.End()

	claims := c.Get("jwtclaims").(util.JwtClaims)
	requester := claims.Audience

	list, err := h.service.ListStreamByAuthor(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, list)
}

// Delete is for handling HTTP Delete Method
func (h handler) Delete(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDelete")
	defer span.End()

	streamID := c.Param("id")
	split := strings.Split(streamID, "@")
	if len(split) == 2 {
		streamID = split[0]
	}

	target, err := h.service.GetStream(ctx, streamID)
	if err != nil {
		span.RecordError(err)
		return err
	}

	claims := c.Get("jwtclaims").(util.JwtClaims)
	requester := claims.Audience

	if target.Author != requester {
		return c.JSON(http.StatusForbidden, echo.Map{"error": "You are not owner of this stream"})
	}

	err = h.service.DeleteStream(ctx, streamID)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.String(http.StatusOK, fmt.Sprintf("{\"message\": \"accept\"}"))
}

// Remove is remove stream element from stream
func (h handler) Remove(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerRemove")
	defer span.End()

	streamID := c.Param("stream")
	split := strings.Split(streamID, "@")
	if len(split) == 2 {
		streamID = split[0]
	}

	objectID := c.Param("object")

	target, err := h.service.GetItem(ctx, streamID, objectID)
	if err != nil {
		span.RecordError(err)
		return err
	}

	claims := c.Get("jwtclaims").(util.JwtClaims)
	requester := claims.Audience

	if target.Author != requester {
		return c.JSON(http.StatusForbidden, echo.Map{"error": "You are not owner of this stream element"})
	}

	h.service.RemoveItem(ctx, streamID, objectID)

	return c.String(http.StatusOK, fmt.Sprintf("{\"message\": \"accept\"}"))
}

// Checkpoint receives events from remote domains
func (h handler) Checkpoint(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerCheckpoint")
	defer span.End()

	var packet checkpointPacket
	err := c.Bind(&packet)
	if err != nil {
		span.RecordError(err)
		return err
	}

	err = h.service.PostItem(ctx, packet.Stream, packet.Item, packet.Body)
	if err != nil {
		span.RecordError(err)
		return nil
	}

	return c.String(http.StatusCreated, fmt.Sprintf("{\"message\": \"accept\"}"))
}
