// Package timeline is for handling concurrent timeline object
package timeline

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/core"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("timeline")

// Handler is the interface for handling HTTP requests
type Handler interface {
	Get(c echo.Context) error
	Recent(c echo.Context) error
	Range(c echo.Context) error
	List(c echo.Context) error
	ListMine(c echo.Context) error
	GetChunks(c echo.Context) error
	Realtime(c echo.Context) error
	Query(c echo.Context) error

	GetChunkItr(c echo.Context) error
	GetChunkBody(c echo.Context) error
	Retracted(c echo.Context) error
}

type handler struct {
	service core.TimelineService
}

// NewHandler creates a new handler
func NewHandler(service core.TimelineService) Handler {
	return &handler{service: service}
}

// Get returns a timeline by ID
func (h handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Timeline.Handler.Get")
	defer span.End()

	timelineID := c.Param("id")
	timeline, err := h.service.GetTimeline(ctx, timelineID)
	if err != nil {
		if errors.Is(err, core.ErrorNotFound{}) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "User not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": timeline})
}

// Recent returns recent messages in some timelines
func (h handler) Recent(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Timeline.Handler.Recent")
	defer span.End()

	timelinesStr := c.QueryParam("timelines")
	timelines := strings.Split(timelinesStr, ",")
	subscription := c.QueryParam("subscription")

	span.SetAttributes(attribute.StringSlice("timelines", timelines))
	span.SetAttributes(attribute.String("subscription", subscription))

	var messages []core.TimelineItem
	var err error
	if subscription != "" {
		messages, err = h.service.GetRecentItemsFromSubscription(ctx, subscription, time.Now(), 16)
	} else {
		messages, err = h.service.GetRecentItems(ctx, timelines, time.Now(), 16)
	}
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": messages})
}

// Range returns messages since to until in specified timelines
func (h handler) Range(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Timeline.Handler.Range")
	defer span.End()

	queryTimelines := c.QueryParam("timelines")
	timelines := strings.Split(queryTimelines, ",")
	querySince := c.QueryParam("since")
	queryUntil := c.QueryParam("until")
	subscription := c.QueryParam("subscription")

	span.SetAttributes(attribute.StringSlice("timelines", timelines))
	span.SetAttributes(attribute.String("subscription", subscription))
	span.SetAttributes(attribute.String("since", querySince))
	span.SetAttributes(attribute.String("until", queryUntil))

	if querySince != "" {
		sinceEpoch, err := strconv.ParseInt(querySince, 10, 64)
		if err != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
		}
		since := time.Unix(sinceEpoch, 0)
		var messages []core.TimelineItem

		if subscription != "" {
			messages, err = h.service.GetImmediateItemsFromSubscription(ctx, subscription, since, 16)
		} else {
			messages, err = h.service.GetImmediateItems(ctx, timelines, since, 16)
		}

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
		var messages []core.TimelineItem

		if subscription != "" {
			messages, err = h.service.GetRecentItemsFromSubscription(ctx, subscription, until, 16)
		} else {
			messages, err = h.service.GetRecentItems(ctx, timelines, until, 16)
		}

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
	ctx, span := tracer.Start(c.Request().Context(), "Timeline.Handler.List")
	defer span.End()

	schema := c.QueryParam("schema")
	list, err := h.service.ListTimelineBySchema(ctx, schema)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": list})
}

// ListMine returns timeline ids which filtered by specific schema
func (h handler) ListMine(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Timeline.Handler.ListMine")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	list, err := h.service.ListTimelineByAuthor(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": list})
}

// GetChunks
func (h handler) GetChunks(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Timeline.Handler.GetChunks")
	defer span.End()

	timelinesStr := c.QueryParam("timelines")
	timelines := strings.Split(timelinesStr, ",")

	timeStr := c.QueryParam("time")
	timeInt, err := strconv.ParseInt(timeStr, 10, 64)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
	}
	time := time.Unix(timeInt, 0)
	epoch := core.Time2Chunk(time)
	fmt.Println("epoch", epoch)

	chunks, err := h.service.GetChunks(ctx, timelines, epoch)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": chunks})
}

func (h handler) GetChunkItr(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Timeline.Handler.GetChunkItr")
	defer span.End()

	timelinesStr := c.QueryParam("timelines")
	timelines := strings.Split(timelinesStr, ",")

	epoch := c.QueryParam("epoch")
	table, err := h.service.LookupChunkItr(ctx, timelines, epoch)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": table})
}

func (h handler) GetChunkBody(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Timeline.Handler.GetChunkBody")
	defer span.End()

	queryStr := c.QueryParam("query")

	query := make(map[string]string)
	for _, kv := range strings.Split(queryStr, ",") {
		pair := strings.Split(kv, ":")
		if len(pair) != 2 {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
		}
		query[pair[0]] = pair[1]
	}

	chunks, err := h.service.LoadChunkBody(ctx, query)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": chunks})
}

func (h handler) Query(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Timeline.Handler.Query")
	defer span.End()

	timelineID := c.Param("id")
	schema := c.QueryParam("schema")
	owner := c.QueryParam("owner")
	author := c.QueryParam("author")
	untilStr := c.QueryParam("until")
	limitStr := c.QueryParam("limit")

	until := time.Now()

	var err error
	if untilStr != "" {
		epoch, err := strconv.ParseInt(untilStr, 10, 64)
		until = time.Unix(epoch, 0)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
		}
	}

	limit := 16
	if limitStr != "" {
		limit, err = strconv.Atoi(limitStr)
		if err != nil {
			span.RecordError(err)
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
		}
	}

	if limit > 100 {
		limit = 100
	}

	items, err := h.service.Query(ctx, timelineID, schema, owner, author, until, limit)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": items})
}

func (h handler) Retracted(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Timeline.Handler.Retracted")
	defer span.End()

	timelinesStr := c.QueryParam("timelines")
	timelines := strings.Split(timelinesStr, ",")

	items, err := h.service.ListLocalRecentlyRemovedItems(ctx, timelines)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": items})
}

// ---

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Request struct {
	Type     string   `json:"type"`
	Channels []string `json:"channels"`
}

func (h handler) Realtime(c echo.Context) error {
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		slog.Error(
			"Failed to upgrade WebSocket",
			slog.String("error", err.Error()),
			slog.String("module", "socket"),
		)
	}
	defer func() {
		ws.Close()
	}()

	ctx := c.Request().Context()

	input := make(chan []string)
	defer close(input)
	output := make(chan core.Event)
	defer close(output)

	go h.service.Realtime(ctx, input, output)

	quit := make(chan struct{})

	go func() {
		for {
			var req Request
			err := ws.ReadJSON(&req)
			if err != nil {
				slog.ErrorContext(
					ctx, "Error reading JSON",
					slog.String("error", err.Error()),
					slog.String("module", "socket"),
				)
				quit <- struct{}{}
				break
			}

			switch req.Type {
			case "listen":
				input <- req.Channels
				slog.DebugContext(
					ctx, fmt.Sprintf("Socket subscribe: %s", req.Channels),
					slog.String("module", "socket"),
				)
			case "h": // heartbeat
				// do nothing
			default:
				slog.InfoContext(
					ctx, "Unknown request type",
					slog.String("type", req.Type),
					slog.String("module", "socket"),
				)
			}
		}
	}()

	for {
		select {
		case <-quit:
			return nil
		case items := <-output:
			err := ws.WriteJSON(items)
			if err != nil {
				slog.ErrorContext(
					ctx, "Error writing message",
					slog.String("error", err.Error()),
					slog.String("module", "socket"),
				)
				return nil
			}
		}
	}
}
