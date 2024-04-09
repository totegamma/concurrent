// Package timeline is for handling concurrent timeline object
package timeline

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("timeline")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	Recent(c echo.Context) error
	Range(c echo.Context) error
	List(c echo.Context) error
	ListMine(c echo.Context) error
	Remove(c echo.Context) error
	Checkpoint(c echo.Context) error
	EventCheckpoint(c echo.Context) error
	GetChunks(c echo.Context) error
}

type handler struct {
	service Service
}

// NewHandler creates a new handler
func NewHandler(service Service) Handler {
	return &handler{service: service}
}

// Get returns a timeline by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGet")
	defer span.End()

	timelineID := c.Param("id")
	timeline, err := h.service.GetTimeline(ctx, timelineID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "User not found"})
		}
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": timeline})
}

// Recent returns recent messages in some timelines
func (h handler) Recent(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerRecent")
	defer span.End()

	timelinesStr := c.QueryParam("timelines")
	timelines := strings.Split(timelinesStr, ",")
	messages, err := h.service.GetRecentItems(ctx, timelines, time.Now(), 16)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": messages})
}

// Range returns messages since to until in specified timelines
func (h handler) Range(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerRange")
	defer span.End()

	queryTimelines := c.QueryParam("timelines")
	timelines := strings.Split(queryTimelines, ",")
	querySince := c.QueryParam("since")
	queryUntil := c.QueryParam("until")

	if querySince != "" {
		sinceEpoch, err := strconv.ParseInt(querySince, 10, 64)
		if err != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
		}
		since := time.Unix(sinceEpoch, 0)
		messages, err := h.service.GetImmediateItems(ctx, timelines, since, 16)

		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}

		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": messages})
	} else if queryUntil != "" {
		untilEpoch, err := strconv.ParseInt(queryUntil, 10, 64)
		if err != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
		}
		until := time.Unix(untilEpoch, 0)
		messages, err := h.service.GetRecentItems(ctx, timelines, until, 16)

		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
		}

		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": messages})
	} else {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
	}
}

// List returns timeline ids which filtered by specific schema
func (h handler) List(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerList")
	defer span.End()

	schema := c.QueryParam("schema")
	list, err := h.service.ListTimelineBySchema(ctx, schema)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": list})
}

// ListMine returns timeline ids which filtered by specific schema
func (h handler) ListMine(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerListMine")
	defer span.End()

	requester, ok := c.Get(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	list, err := h.service.ListTimelineByAuthor(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": list})
}

// Remove is remove timeline element from timeline
func (h handler) Remove(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerRemove")
	defer span.End()

	timelineID := c.Param("timeline")
	split := strings.Split(timelineID, "@")
	if len(split) == 2 {
		timelineID = split[0]
	}

	objectID := c.Param("object")

	target, err := h.service.GetItem(ctx, timelineID, objectID)
	if err != nil {
		span.RecordError(err)
		return err
	}

	requester, ok := c.Get(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	if *target.Author != requester && target.Owner != requester {
		return c.JSON(http.StatusForbidden, echo.Map{"error": "You are not owner of this timeline element"})
	}

	h.service.RemoveItem(ctx, timelineID, objectID)

	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
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

	requesterDomain, ok := c.Get(core.RequesterDomainCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester domain not found"})
	}

	err = h.service.Checkpoint(ctx, packet.Timeline, packet.Item, packet.Body, packet.Principal, requesterDomain)
	if err != nil {
		span.RecordError(err)
		return nil
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}

func (h handler) EventCheckpoint(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerEventCheckpoint")
	defer span.End()

	var event core.Event
	err := c.Bind(&event)
	if err != nil {
		span.RecordError(err)
		return err
	}

	err = h.service.PublishEventToLocal(ctx, event)
	if err != nil {
		span.RecordError(err)
		return nil
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}

// GetChunks
func (h handler) GetChunks(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetChunks")
	defer span.End()

	timelinesStr := c.QueryParam("timelines")
	timelines := strings.Split(timelinesStr, ",")

	timeStr := c.QueryParam("time")
	timeInt, err := strconv.ParseInt(timeStr, 10, 64)
	if err != nil {
		span.RecordError(err)
		return err
	}
	time := time.Unix(timeInt, 0)

	chunks, err := h.service.GetChunks(ctx, timelines, time)
	if err != nil {
		span.RecordError(err)
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": chunks})
}
