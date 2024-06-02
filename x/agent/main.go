// Package agent runs some scheduled tasks
package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/totegamma/concurrent/core"
)

var tracer = otel.Tracer("agent")

type agent struct {
	mc             *memcache.Client
	rdb            *redis.Client
	store          core.StoreService
	job            core.JobService
	config         core.Config
	repositoryPath string
}

// NewAgent creates a new agent
func NewAgent(
	mc *memcache.Client,
	rdb *redis.Client,
	store core.StoreService,
	job core.JobService,
	config core.Config,
	repositoryPath string,
) core.AgentService {
	return &agent{
		mc,
		rdb,
		store,
		job,
		config,
		repositoryPath,
	}
}

// Boot starts agent
func (a *agent) Boot() {
	slog.Info("agent start!")

	ctx := context.Background()

	go a.watchEventRoutine(ctx)
	go a.chunkUpdaterRoutine(ctx)
	go a.connectionKeeperRoutine(ctx)

	ticker60A := time.NewTicker(60 * time.Second)
	go func() {
		for {
			select {
			case <-ticker60A.C:
				ctx, span := tracer.Start(context.Background(), "Agent.Boot.FlushLog")
				a.FlushLog(ctx)
				span.End()
				break
			}
		}
	}()

	ticker60B := time.NewTicker(60 * time.Second)
	go func() {
		for {
			select {
			case <-ticker60B.C:
				ctx, span := tracer.Start(context.Background(), "Agent.Boot.DispatchJobs")
				a.dispatchJobs(ctx)
				span.End()
				break
			}
		}
	}()

}
