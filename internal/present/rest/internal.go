package rest

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/concrnt/concrnt/internal/infra/cluster"
)

// LocalDemand reports the realtime subscription demand of this replica.
type LocalDemand interface {
	CurrentSubscriptions() []string
}

// LeaderState tells whether this replica currently runs the singleton workers.
type LeaderState interface {
	IsLeader() bool
}

// InternalHandler serves the replica-to-replica coordination API. It must be
// bound to a listener that is not exposed outside the cluster.
type InternalHandler struct {
	demand  LocalDemand
	ensurer cluster.Ensurer
	leader  LeaderState
}

func NewInternalHandler(demand LocalDemand, ensurer cluster.Ensurer, leader LeaderState) *InternalHandler {
	return &InternalHandler{
		demand:  demand,
		ensurer: ensurer,
		leader:  leader,
	}
}

func (h *InternalHandler) RegisterRoutes(e *echo.Echo) {
	e.GET("/internal/subscriptions", h.handleSubscriptions)
	e.POST("/internal/subscriptions/ensure", h.handleEnsure)
	e.GET("/health", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
}

// handleSubscriptions returns the prefixes wanted by this replica's realtime
// sessions. The leader polls this on every replica to build the cluster-wide
// demand set.
func (h *InternalHandler) handleSubscriptions(c echo.Context) error {
	prefixes := h.demand.CurrentSubscriptions()
	if prefixes == nil {
		prefixes = []string{}
	}
	return c.JSON(http.StatusOK, prefixes)
}

// handleEnsure applies subscription demand immediately. Only the leader runs
// the Subscriber, so non-leaders reject the request; the caller falls back to
// the periodic poll.
func (h *InternalHandler) handleEnsure(c echo.Context) error {
	if !h.leader.IsLeader() {
		return c.String(http.StatusConflict, "not the leader")
	}

	var req cluster.EnsureRequest
	if err := c.Bind(&req); err != nil {
		return c.String(http.StatusBadRequest, "invalid request")
	}

	h.ensurer.EnsureSubscriptions(c.Request().Context(), req.Prefixes)
	return c.String(http.StatusOK, "ok")
}

// StartInternalListener runs the internal coordination API on its own port,
// shutting down when ctx is cancelled.
func StartInternalListener(ctx context.Context, addr string, handler *InternalHandler) *echo.Echo {
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

	return e
}
