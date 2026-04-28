package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"

	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/impl/interop"
	"github.com/totegamma/concrnt-playground/internal/domain"
	"github.com/totegamma/concrnt-playground/internal/present/rest/presenter"
	"github.com/totegamma/concrnt-playground/internal/service"
	"github.com/totegamma/concrnt-playground/internal/usecase"
	"github.com/totegamma/concrnt-playground/schemas"
)

type Handler struct {
	config    domain.Config
	record    *usecase.RecordUsecase
	chunkline *usecase.ChunklineUsecase
	server    *usecase.ServerUsecase
	entity    *usecase.EntityUsecase
	notify    *usecase.NotificationUsecase
	abuse     *service.AbuseService
	signal    *service.SignalService
	mm        *service.ModuleManager
}

func NewHandler(
	config domain.Config,
	record *usecase.RecordUsecase,
	chunkline *usecase.ChunklineUsecase,
	server *usecase.ServerUsecase,
	entity *usecase.EntityUsecase,
	notify *usecase.NotificationUsecase,
	abuse *service.AbuseService,
	signal *service.SignalService,
	mm *service.ModuleManager,
) *Handler {
	return &Handler{
		config:    config,
		record:    record,
		chunkline: chunkline,
		server:    server,
		entity:    entity,
		notify:    notify,
		abuse:     abuse,
		signal:    signal,
		mm:        mm,
	}
}

var Endpoints = map[string]string{
	"net.concrnt.core.commit":             "/commit",
	"net.concrnt.core.resolve":            "/resolve?uri={uri}",
	"net.concrnt.core.query":              "/query{?prefix,schema,since,until,limit,order,parent}",
	"net.concrnt.core.associations":       "/associations{?uri,schema,variant,author}",
	"net.concrnt.core.association-counts": "/association-counts{?uri,schema}",
	"net.concrnt.core.acknowledges":       "/acknowledges{?from,to,context}",
	"net.concrnt.core.acknowledge-counts": "/acknowledge-counts{?from,to,context}",
	"net.concrnt.core.realtime":           "/realtime",
	"net.concrnt.core.abuse":              "/abuse",
	"net.concrnt.world.register":          "/api/v2/register",
	"net.concrnt.world.timeline.recent":   "/api/v2/timeline/recent{?uris,until,limit}",
	"net.concrnt.world.subscribe":         "/subscribe/{owner}/{vendor_id}",
	"net.concrnt.world.repository":        "/repository",
	"net.concrnt.core.known-servers":      "/known-servers",
}

func (h *Handler) RegisterRoutes(e *echo.Group) {

	api := e.Group("", echomiddleware.CORS())
	api.POST("/commit", h.handleCommit)
	api.OPTIONS("/commit", h.handleNop)
	api.GET("/resolve", h.handleResolve)
	api.OPTIONS("/resolve", h.handleNop)
	api.GET("/query", h.handleQuery)
	api.OPTIONS("/query", h.handleNop)
	api.GET("/associations", h.handleAssociations)
	api.OPTIONS("/associations", h.handleNop)
	api.GET("/association-counts", h.handleAssociationCounts)
	api.OPTIONS("/association-counts", h.handleNop)
	api.GET("/acknowledges", h.handleAcknowledges)
	api.OPTIONS("/acknowledges", h.handleNop)
	api.GET("/acknowledge-counts", h.handleAcknowledgeCounts)
	api.OPTIONS("/acknowledge-counts", h.handleNop)
	api.GET("/realtime", h.handleRealtime)
	api.OPTIONS("/realtime", h.handleNop)
	api.POST("/api/v2/register", h.handleRegister)
	api.OPTIONS("/api/v2/register", h.handleNop)
	api.GET("/api/v2/timeline/recent", h.handleTimelineRecent)
	api.OPTIONS("/api/v2/timeline/recent", h.handleNop)
	api.POST("/subscribe/:owner/:vendor_id", h.handleSubscribeNotification)
	api.OPTIONS("/subscribe/:owner/:vendor_id", h.handleNop)
	api.GET("/subscribe/:owner/:vendor_id", h.handleGetNotification)
	api.DELETE("/subscribe/:owner/:vendor_id", h.handleDeleteNotification)
	api.GET("/known-servers", h.handleKnownServers)
	api.OPTIONS("/known-servers", h.handleNop)
	api.GET("/repository", h.handleDumpRepository)
	api.POST("/repository", h.handleImportRepository)
	api.OPTIONS("/repository", h.handleNop)
	api.POST("/abuse", h.handleAbuse)
	api.OPTIONS("/abuse", h.handleNop)

	api.GET("/chunkline/itr/:chunk", h.handleChunklineItr)
	api.OPTIONS("/chunkline/itr/:chunk", h.handleNop)
	api.GET("/chunkline/body/:chunk", h.handleChunklineBody)
	api.OPTIONS("/chunkline/body/:chunk", h.handleNop)

	// internal
	api.GET("/internal/signal/subscriptions", h.handleCurrentSubs)
	api.OPTIONS("/internal/signal/subscriptions", h.handleNop)

	api.GET("/tos", func(c echo.Context) (err error) {
		return c.File("/etc/concrnt/static/tos.txt")
	})
	api.OPTIONS("/tos", h.handleNop)

	api.GET("/code-of-conduct", func(c echo.Context) (err error) {
		return c.File("/etc/concrnt/static/code-of-conduct.txt")
	})
	api.OPTIONS("/code-of-conduct", h.handleNop)

	api.GET("/register-template", func(c echo.Context) (err error) {
		return c.File("/etc/concrnt/static/register-template.json")
	})
	api.OPTIONS("/register-template", h.handleNop)

}

func (h *Handler) handleNop(c echo.Context) error {
	return c.NoContent(http.StatusOK)
}

func (h *Handler) handleCommit(c echo.Context) error {
	ctx := c.Request().Context()

	var sd concrnt.SignedDocument
	err := c.Bind(&sd)
	if err != nil {
		return presenter.BadRequest(c, err)
	}

	ip := c.RealIP()

	result, err := h.record.Commit(ctx, ip, sd, domain.CommitModeExecute)
	if err != nil {
		if errors.Is(err, domain.ErrPermissionDenied) {
			return presenter.Forbidden(c, "permission denied")
		}
		return presenter.InternalError(c, err)
	}

	return presenter.OK(c, result)
}

func (h *Handler) handleResolve(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Handler.handleResource")
	defer span.End()

	uriEscaped := c.QueryParam("uri")
	uriString, err := url.PathUnescape(uriEscaped)
	if err != nil {
		return presenter.BadRequestMessage(c, "invalid uri")
	}

	uri, err := url.Parse(uriString)
	if err != nil {
		return presenter.BadRequestMessage(c, "invalid uri")
	}

	if uri.Scheme == "http" || uri.Scheme == "https" {
		return c.JSON(http.StatusSeeOther, echo.Map{"location": uri.String()})
	}

	parsed, err := concrnt.ParseCCURI(uriString)
	if err != nil {
		return presenter.BadRequestMessage(c, "invalid uri")
	}

	if parsed.Scheme == "ccfs" && strings.HasPrefix(parsed.CDID, "sha256-") {
		// redirect to
		endpoints := h.mm.GetEndpoints()
		storageModule, ok := endpoints["net.concrnt.storage.resolve"]
		if !ok {
			return presenter.InternalError(c, errors.New("storage module not found"))
		}

		path, err := concrnt.RenderURITemplate(storageModule, map[string]string{
			"hash": parsed.CDID,
		})
		if err != nil {
			return presenter.InternalError(c, err)
		}

		c.Response().Header().Set("Location", path)
		return c.JSON(http.StatusSeeOther, echo.Map{"location": path})
	}

	if parsed.Key == "" && parsed.CDID == "" {
		if concrnt.IsCCID(parsed.Owner) {
			entity, err := h.entity.GetSD(ctx, parsed.Owner, parsed.Hint)
			if err != nil {
				if errors.Is(err, domain.ErrPermissionDenied) {
					return presenter.Forbidden(c, "permission denied") // TODO: should be return NotFound
				}
				if errors.Is(err, domain.ErrNotFound) {
					return presenter.NotFound(c, "resource not found")
				}
				return presenter.InternalError(c, err)
			}
			return presenter.OK(c, entity)
		}

		wkc, err := h.server.Resolve(ctx, parsed.Owner, parsed.Hint)
		if err != nil {
			if errors.Is(err, domain.ErrRedirect) {
				redirectErr := err.(domain.RedirectError)
				return presenter.Redirect(c, redirectErr.Location)
			}
			if errors.Is(err, domain.ErrPermissionDenied) {
				return presenter.Forbidden(c, "permission denied") // TODO: should be return NotFound
			}
			return presenter.InternalError(c, err)
		}
		return presenter.OK(c, wkc)
	}

	accept := c.Request().Header.Get("Accept")

	switch accept {
	case "application/chunkline+json":
		value, err := h.chunkline.GetChunklineManifest(ctx, uriString)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return presenter.NotFound(c, "resource not found")
			}
			if errors.Is(err, domain.ErrPermissionDenied) {
				return presenter.Forbidden(c, "permission denied") // TODO: should be return NotFound
			}
			return presenter.InternalError(c, err)
		}
		return presenter.OK(c, value)
	default:
		value, err := h.record.GetSigned(ctx, uri.String())
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return presenter.NotFound(c, "resource not found")
			}
			if errors.Is(err, domain.ErrPermissionDenied) {
				return presenter.Forbidden(c, "permission denied") // TODO: should be return NotFound
			}
			return presenter.InternalError(c, err)
		}

		var doc concrnt.Document[schemas.Reference]
		err = json.Unmarshal([]byte(value.Document), &doc)
		if err != nil {
			return presenter.OK(c, value)
		}

		if doc.Schema == schemas.ReferenceURL {
			c.Response().Header().Set("Location", "/resolve?uri="+url.PathEscape(doc.Value.Href))
			return c.JSON(http.StatusFound, value)
		}

		return presenter.OK(c, value)
	}

}

func (h *Handler) handleQuery(c echo.Context) error {
	ctx := c.Request().Context()

	prefix := c.QueryParam("prefix")
	parent := c.QueryParam("parent")

	schema := c.QueryParam("schema")

	var since *time.Time
	sinceStr := c.QueryParam("since")
	if sinceStr != "" {
		parsed, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return presenter.BadRequestMessage(c, "invalid since parameter")
		}
		since = &parsed
	}

	var until *time.Time
	untilStr := c.QueryParam("until")
	if untilStr != "" {
		parsed, err := time.Parse(time.RFC3339, untilStr)
		if err != nil {
			return presenter.BadRequestMessage(c, "invalid until parameter")
		}
		until = &parsed
	}

	limit := 10
	limitStr := c.QueryParam("limit")
	if limitStr != "" {
		limitInt, err := strconv.Atoi(limitStr)
		if err != nil {
			return presenter.BadRequestMessage(c, "invalid limit parameter")
		}
		limit = limitInt
	}
	if limit > 100 {
		limit = 100
	}

	order := c.QueryParam("order")
	if order == "" {
		order = "desc"
	} else if order != "asc" && order != "desc" {
		return presenter.BadRequestMessage(c, "invalid order parameter")
	}

	results, err := h.record.Query(ctx, prefix, parent, schema, since, until, limit, order)
	if err != nil {
		return presenter.InternalError(c, err)
	}
	return presenter.OK(c, results)
}

func (h *Handler) handleChunklineItr(c echo.Context) error {
	ctx := c.Request().Context()
	uri := c.QueryParam("uri")

	chunkID, err := strconv.ParseInt(c.Param("chunk"), 10, 64)
	if err != nil {
		return presenter.BadRequestMessage(c, "invalid chunk id")
	}

	results, err := h.chunkline.LookupLocalItrs(ctx, []string{uri}, chunkID)
	if err != nil {
		return presenter.InternalError(c, err)
	}

	return c.String(http.StatusOK, strconv.FormatInt(results[uri], 10))
}

func (h *Handler) handleChunklineBody(c echo.Context) error {
	ctx := c.Request().Context()
	uri := c.QueryParam("uri")

	chunkID, err := strconv.ParseInt(c.Param("chunk"), 10, 64)
	if err != nil {
		return presenter.BadRequestMessage(c, "invalid chunk id")
	}
	results, err := h.chunkline.LoadLocalBody(ctx, uri, chunkID)
	if err != nil {
		return presenter.InternalError(c, err)
	}
	return presenter.OK(c, results)
}

func (h *Handler) handleRegister(c echo.Context) error {
	ctx := c.Request().Context()
	var req concrnt.RegisterRequest[domain.EntityMeta]
	err := c.Bind(&req)
	if err != nil {
		return presenter.BadRequest(c, err)
	}

	err = h.entity.Register(ctx, req)
	if err != nil {
		return presenter.InternalError(c, err)
	}
	return presenter.OK(c, echo.Map{"status": "ok"})
}

func (h *Handler) handleTimelineRecent(c echo.Context) error {
	ctx := c.Request().Context()
	uriString := c.QueryParam("uris")
	if uriString == "" {
		return presenter.OK(c, []any{})
	}
	uris := strings.Split(uriString, ",")
	untilStr := c.QueryParam("until")
	var until time.Time
	if untilStr == "" {
		until = time.Now().UTC()
	} else {
		untilInt, err := strconv.ParseInt(untilStr, 10, 64)
		if err != nil {
			return presenter.BadRequestMessage(c, "invalid until parameter")
		}
		until = time.UnixMilli(untilInt).UTC()
	}
	limit := 16
	limitStr := c.QueryParam("limit")
	if limitStr != "" {
		limitInt, err := strconv.Atoi(limitStr)
		if err != nil {
			return presenter.BadRequestMessage(c, "invalid limit parameter")
		}
		limit = limitInt
	}
	if limit > 64 {
		limit = 64
	}

	results, err := h.chunkline.GetRecent(ctx, uris, until, limit)
	if err != nil {
		return presenter.InternalError(c, err)
	}
	return presenter.OK(c, results)
}

func (h *Handler) handleSubscribeNotification(c echo.Context) error {
	ctx := c.Request().Context()

	owner, err := url.PathUnescape(c.Param("owner"))
	if err != nil {
		return presenter.BadRequestMessage(c, "invalid owner")
	}
	vendorID, err := url.PathUnescape(c.Param("vendor_id"))
	if err != nil {
		return presenter.BadRequestMessage(c, "invalid vendor_id")
	}

	if err := requireNotificationOwner(ctx, owner); err != nil {
		return presenter.Forbidden(c, err.Error())
	}

	var subscription domain.NotificationSubscription
	if err := c.Bind(&subscription); err != nil {
		return presenter.BadRequest(c, err)
	}

	subscription.Owner = owner
	subscription.VendorID = vendorID

	if len(subscription.Prefixes) == 0 {
		return presenter.BadRequestMessage(c, "prefixes must not be empty")
	}
	if subscription.Subscription == "" {
		return presenter.BadRequestMessage(c, "subscription must not be empty")
	}

	subscription, err = h.notify.Subscribe(ctx, subscription)
	if err != nil {
		return presenter.InternalError(c, err)
	}

	return c.JSON(http.StatusCreated, echo.Map{"status": "ok", "content": subscription})
}

func (h *Handler) handleGetNotification(c echo.Context) error {
	ctx := c.Request().Context()

	owner, err := url.PathUnescape(c.Param("owner"))
	if err != nil {
		return presenter.BadRequestMessage(c, "invalid owner")
	}
	vendorID, err := url.PathUnescape(c.Param("vendor_id"))
	if err != nil {
		return presenter.BadRequestMessage(c, "invalid vendor_id")
	}

	if err := requireNotificationOwner(ctx, owner); err != nil {
		return presenter.Forbidden(c, err.Error())
	}

	subscription, err := h.notify.Get(ctx, vendorID, owner)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return presenter.NotFound(c, "notification subscription not found")
		}
		return presenter.InternalError(c, err)
	}

	return presenter.OK(c, echo.Map{"status": "ok", "content": subscription})
}

func (h *Handler) handleDeleteNotification(c echo.Context) error {
	ctx := c.Request().Context()

	owner, err := url.PathUnescape(c.Param("owner"))
	if err != nil {
		return presenter.BadRequestMessage(c, "invalid owner")
	}
	vendorID, err := url.PathUnescape(c.Param("vendor_id"))
	if err != nil {
		return presenter.BadRequestMessage(c, "invalid vendor_id")
	}

	if err := requireNotificationOwner(ctx, owner); err != nil {
		return presenter.Forbidden(c, err.Error())
	}

	err = h.notify.Delete(ctx, vendorID, owner)
	if err != nil {
		return presenter.InternalError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

func requireNotificationOwner(ctx context.Context, owner string) error {
	requester, ok := ctx.Value(interop.RequesterCtxKey).(domain.Entity)
	if !ok {
		return domain.PermissionError{Reason: "authentication required"}
	}
	if requester.ID != owner {
		return domain.PermissionError{Reason: "owner mismatch"}
	}
	return nil
}

func (h *Handler) handleAssociations(c echo.Context) error {
	ctx := c.Request().Context()

	uri := c.QueryParam("uri")
	schema := c.QueryParam("schema")
	variant := c.QueryParam("variant")
	author := c.QueryParam("author")

	if uri == "" {
		return presenter.BadRequestMessage(c, "uri parameter is required")
	}

	records, err := h.record.GetAssociatedRecords(ctx, uri, schema, variant, author)
	if err != nil {
		return presenter.InternalError(c, err)
	}
	return presenter.OK(c, records)

}

func (h *Handler) handleAssociationCounts(c echo.Context) error {
	ctx := c.Request().Context()

	uri := c.QueryParam("uri")
	schema := c.QueryParam("schema")

	if uri == "" {
		return presenter.BadRequestMessage(c, "uri parameter is required")
	}

	if schema == "" {
		counts, err := h.record.GetAssociatedRecordCountsBySchema(ctx, uri)
		if err != nil {
			return presenter.InternalError(c, err)
		}
		return presenter.OK(c, counts)
	} else {
		counts, err := h.record.GetAssociatedRecordCountsByVariant(ctx, uri, schema)
		if err != nil {
			return presenter.InternalError(c, err)
		}
		return presenter.OK(c, counts)
	}

}

func (h *Handler) handleCurrentSubs(c echo.Context) error {
	subs := h.signal.GetCurrentSubscriptions()
	return presenter.OK(c, subs)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (h *Handler) handleRealtime(c echo.Context) error {
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		slog.Error(
			"Failed to upgrade WebSocket",
			slog.String("error", err.Error()),
			slog.String("module", "socket"),
		)
		return err
	}
	defer func() {
		ws.Close()
	}()

	ctx := c.Request().Context()

	input := make(chan []string)
	defer close(input)
	output := make(chan concrnt.Event)
	defer close(output)

	go h.signal.Realtime(ctx, input, output)

	quit := make(chan struct{})

	go func() {
		for {
			var req concrnt.RealtimeRequest
			err := ws.ReadJSON(&req)
			if err != nil {

				wsErr, ok := err.(*websocket.CloseError)
				if ok {
					if !(wsErr.Code == websocket.CloseNormalClosure || wsErr.Code == websocket.CloseGoingAway) {
						slog.DebugContext(
							ctx, "WebSocket closed",
							slog.String("error", wsErr.Error()),
							slog.String("module", "socket"),
						)
					}
				} else {
					slog.ErrorContext(
						ctx, "Error reading message",
						slog.String("error", err.Error()),
						slog.String("module", "socket"),
					)
				}

				quit <- struct{}{}
				break
			}

			switch req.Type {
			case "listen":
				input <- req.Prefixes
				slog.DebugContext(
					ctx, fmt.Sprintf("Socket subscribe: %s", req.Prefixes),
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

func (h *Handler) handleKnownServers(c echo.Context) error {
	ctx := c.Request().Context()

	servers, err := h.server.List(ctx)
	if err != nil {
		return presenter.InternalError(c, err)
	}
	return presenter.OK(c, servers)
}

func (h *Handler) handleAcknowledges(c echo.Context) error {
	ctx := c.Request().Context()

	from := c.QueryParam("from")
	to := c.QueryParam("to")
	context := c.QueryParam("context")

	if from == "" && to == "" {
		return presenter.BadRequestMessage(c, "from and to parameters are required")
	}

	records, err := h.record.GetAcknowledgeRecords(ctx, from, to, context)
	if err != nil {
		return presenter.InternalError(c, err)
	}
	return presenter.OK(c, records)
}

func (h *Handler) handleAcknowledgeCounts(c echo.Context) error {
	ctx := c.Request().Context()

	from := c.QueryParam("from")
	to := c.QueryParam("to")
	context := c.QueryParam("context")

	if from == "" && to == "" {
		return presenter.BadRequestMessage(c, "from and to parameters are required")
	}

	counts, err := h.record.GetAcknowledgeRecordCounts(ctx, from, to, context)
	if err != nil {
		return presenter.InternalError(c, err)
	}
	return presenter.OK(c, counts)
}

func (h *Handler) handleDumpRepository(c echo.Context) error {
	ctx := c.Request().Context()

	dump, err := h.record.DumpCommitLogs(ctx)
	if err != nil {
		return presenter.InternalError(c, err)
	}
	return c.String(http.StatusOK, dump)
}

func (h *Handler) handleImportRepository(c echo.Context) error {
	ctx := c.Request().Context()

	var dump string
	dumpBytes, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return presenter.BadRequest(c, err)
	}
	dump = string(dumpBytes)

	ip := c.RealIP()

	results := h.record.ImportCommitLogs(ctx, ip, dump)
	return presenter.OK(c, results)
}

func (h *Handler) handleAbuse(c echo.Context) error {
	ctx := c.Request().Context()

	var req concrnt.AbuseReport
	err := c.Bind(&req)
	if err != nil {
		return presenter.BadRequest(c, err)
	}

	requester, ok := ctx.Value(interop.RequesterCtxKey).(domain.Entity)
	if !ok {
		return presenter.Forbidden(c, "authentication required")
	}

	reporter := requester.ID

	ip := c.RealIP()

	err = h.abuse.ReportAbuse(ctx, &req, reporter, ip)
	if err != nil {
		return presenter.InternalError(c, err)
	}

	return presenter.OK(c, echo.Map{"status": "ok"})
}
