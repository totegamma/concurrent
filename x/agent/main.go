// Package agent runs some scheduled tasks
package agent

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"io"
	"log"
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
	log.Printf("agent start!")
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

	var remoteEntities []core.Entity
	json.Unmarshal(body, &remoteEntities)

	errored := false
	for _, entity := range remoteEntities {

        util.VerifySignature(entity.Payload, entity.ID, entity.Signature)

        var signedObj core.SignedObject
        err := json.Unmarshal([]byte(entity.Payload), &signedObj)
        if err != nil {
            span.RecordError(err)
            log.Println(err)
            continue
        }

        existance, err := a.entity.GetAddress(ctx, entity.ID)
        if err == nil {
            // compare signed date
            if signedObj.SignedAt.Unix() <= existance.SignedAt.Unix() {
                continue
            }
        }

        err = a.entity.UpdateAddress(ctx, entity.ID, remote.ID, signedObj.SignedAt)

		if err != nil {
			span.RecordError(err)
			errored = true
			log.Println(err)
		}
	}

	if !errored {
		log.Printf("[agent] pulled %d entities from %s", len(remoteEntities), remote.ID)
		a.domain.UpdateScrapeTime(ctx, remote.ID, requestTime)
	} else {
		log.Printf("[agent] failed to pull entities from %s", remote.ID)
	}

	return nil
}

