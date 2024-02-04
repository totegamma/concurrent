// Package agent runs some scheduled tasks
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"
)

var tracer = otel.Tracer("agent")

// Agent is the worker that runs scheduled tasks
// - collect users from other servers
// - update socket connections
type Agent interface {
	Boot()
}

type agent struct {
	rdb         *redis.Client
	config      util.Config
	domain      domain.Service
	entity      entity.Service
	mutex       *sync.Mutex
	connections map[string]*websocket.Conn
}

// NewAgent creates a new agent
func NewAgent(rdb *redis.Client, config util.Config, domain domain.Service, entity entity.Service) Agent {
	return &agent{
		rdb,
		config,
		domain,
		entity,
		&sync.Mutex{},
		make(map[string]*websocket.Conn),
	}
}

func (a *agent) collectUsers(ctx context.Context) {
	hosts, err := a.domain.List(ctx)
	if err != nil || len(hosts) == 0 {
		return
	}
	host := hosts[rand.Intn(len(hosts))]
	a.pullRemoteEntities(ctx, host)
}

// Boot starts agent
func (a *agent) Boot() {
	slog.Info("agent start!")
	ticker60 := time.NewTicker(60 * time.Second)
	go func() {
		for {
			select {
			case <-ticker60.C:
				ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
				defer cancel()
				a.collectUsers(ctx)
				break
			}
		}
	}()
}

type entitiesResponse struct {
	Status  string        `json:"status"`
	Content []core.Entity `json:"content"`
}

// PullRemoteEntities copies remote entities
func (a *agent) pullRemoteEntities(ctx context.Context, remote core.Domain) error {
	ctx, span := tracer.Start(ctx, "ServicePullRemoteEntities")
	defer span.End()

	requestTime := time.Now()
	req, err := http.NewRequest("GET", "https://"+remote.ID+"/api/v1/entities?since="+strconv.FormatInt(remote.LastScraped.Unix(), 10), nil) // TODO: add except parameter
	if err != nil {
		span.RecordError(err)
		return err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := new(http.Client)
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var remoteEntities entitiesResponse
	json.Unmarshal(body, &remoteEntities)

	errored := false
	for _, entity := range remoteEntities.Content {

		err := util.VerifySignature(entity.Payload, entity.ID, entity.Signature)
		if err != nil {
			span.RecordError(err)
			slog.Error(
				"Invalid signature",
				slog.String("error", err.Error()),
				slog.String("module", "agent"),
			)
			continue
		}

		var signedObj core.SignedObject
		err = json.Unmarshal([]byte(entity.Payload), &signedObj)
		if err != nil {
			span.RecordError(err)
			slog.Error(
				"pullRemoteEntities",
				slog.String("error", err.Error()),
				slog.String("module", "agent"),
			)
			continue
		}

		existanceAddr, err := a.entity.GetAddress(ctx, entity.ID)
		if err == nil {
			// compare signed date
			if signedObj.SignedAt.Unix() <= existanceAddr.SignedAt.Unix() {
				continue
			}
		}

		existanceEntity, err := a.entity.Get(ctx, entity.ID)
		if err == nil {
			if signedObj.SignedAt.Unix() <= existanceEntity.CDate.Unix() {
				continue
			}
		}

		err = a.entity.UpdateAddress(ctx, entity.ID, remote.ID, signedObj.SignedAt)

		if err != nil {
			span.RecordError(err)
			slog.Error(
				"pullRemoteEntities",
				slog.String("error", err.Error()),
				slog.String("module", "agent"),
			)
			errored = true
		}
	}

	if !errored {
		slog.Info(
			fmt.Sprintf("[agent] pulled %d entities from %s", len(remoteEntities.Content), remote.ID),
			slog.String("module", "agent"),
		)
		a.domain.UpdateScrapeTime(ctx, remote.ID, requestTime)
	} else {
		slog.Error(
			fmt.Sprintf("[agent] failed to pull entities from %s", remote.ID),
			slog.String("module", "agent"),
		)
	}

	return nil
}
