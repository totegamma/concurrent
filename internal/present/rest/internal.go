package rest

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/concrnt/concrnt/internal/worker"
)

// LocalDemand reports the realtime subscription demand of this replica's own
// registered clients only — no cluster awareness. This is what a leader polls
// on every replica to build the cluster-wide demand set, so it must never
// itself forward to another replica (that would loop leader->peer->leader).
type LocalDemand interface {
	CurrentSubscriptions() []string
}

// AggregateDemand reports the realtime subscription demand across the whole
// cluster, as tracked by whichever replica currently runs the singleton
// subscriber (the leader).
type AggregateDemand interface {
	CurrentSubscriptions() []string
}

// Ensurer requests that upstream subscriptions cover the given prefixes; only
// meaningful on the leader, which is the only replica that dials upstream.
type Ensurer interface {
	EnsureSubscriptions(ctx context.Context, prefixes []string)
}

// LeaderState tells whether this replica currently runs the singleton workers.
type LeaderState interface {
	IsLeader() bool
}

// InternalHandler serves the replica-to-replica coordination API. It must be
// bound to a listener that is not exposed outside the cluster.
type InternalHandler struct {
	local     LocalDemand
	aggregate AggregateDemand
	ensurer   Ensurer
	leader    LeaderState
}

func NewInternalHandler(local LocalDemand, aggregate AggregateDemand, ensurer Ensurer, leader LeaderState) *InternalHandler {
	return &InternalHandler{
		local:     local,
		aggregate: aggregate,
		ensurer:   ensurer,
		leader:    leader,
	}
}

func (h *InternalHandler) RegisterRoutes(e *echo.Echo) {
	e.GET("/internal/demand", h.handleDemand)
	e.GET("/internal/subscriptions", h.handleSubscriptions)
	e.POST("/internal/subscriptions/ensure", h.handleEnsure)
	e.GET("/health", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
}

// handleDemand returns the prefixes wanted by this replica's own realtime
// sessions. The leader polls this on every replica (including itself) to
// build the cluster-wide demand set.
func (h *InternalHandler) handleDemand(c echo.Context) error {
	prefixes := h.local.CurrentSubscriptions()
	if prefixes == nil {
		prefixes = []string{}
	}
	return c.JSON(http.StatusOK, prefixes)
}

// handleSubscriptions returns the cluster-wide aggregate subscription state.
// Only the leader can answer this — it is the only replica that actually
// knows it, and forwarding would let two GET /internal/subscriptions handlers
// loop off each other during a leadership handover.
func (h *InternalHandler) handleSubscriptions(c echo.Context) error {
	if !h.leader.IsLeader() {
		return c.String(http.StatusConflict, "not the leader")
	}

	prefixes := h.aggregate.CurrentSubscriptions()
	if prefixes == nil {
		prefixes = []string{}
	}
	return c.JSON(http.StatusOK, prefixes)
}

// handleEnsure applies subscription demand immediately. Only the leader runs
// the Subscriber, so non-leaders reject the request; the caller falls back to
// the periodic peer poll.
func (h *InternalHandler) handleEnsure(c echo.Context) error {
	if !h.leader.IsLeader() {
		return c.String(http.StatusConflict, "not the leader")
	}

	var req worker.EnsureRequest
	if err := c.Bind(&req); err != nil {
		return c.String(http.StatusBadRequest, "invalid request")
	}

	h.ensurer.EnsureSubscriptions(c.Request().Context(), req.Prefixes)
	return c.String(http.StatusOK, "ok")
}

// StartInternalListener runs the internal coordination API on its own port,
// shutting down when ctx is cancelled.
func StartInternalListener(ctx context.Context, addr string, handler *InternalHandler) {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	handler.RegisterRoutes(e)

	go func() {
		if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
			// the coordination plane is degraded but the public API can keep serving
			e.Logger.Error(err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.Shutdown(shutdownCtx)
	}()
}
