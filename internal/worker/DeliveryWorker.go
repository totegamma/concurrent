package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
)

// deliveryConcurrency is the number of consumer goroutines the worker asks
// the queue to run concurrently.
const deliveryConcurrency = 4

// DeliveryQueue is the consumer-side view of a delivery job queue: run
// starts draining it, invoking handler for each job until ctx is cancelled.
type DeliveryQueue interface {
	Run(ctx context.Context, concurrency int, handler func(ctx context.Context, job domain.DeliveryJob) error) error
}

// DeliveryClient is the subset of client.Client that DeliveryWorker needs.
type DeliveryClient interface {
	ResolveResourceHost(ctx context.Context, uri string) (string, error)
	Commit(ctx context.Context, resolver string, sd concrnt.SignedDocument) error
}

// Committer is satisfied by *usecase.RecordUsecase; it lets a job whose
// destination turns out to be local re-enter the normal commit path.
type Committer interface {
	Commit(ctx context.Context, ip string, sd concrnt.SignedDocument, mode domain.CommitMode) (*concrnt.SignedDocument, error)
}

// DeliveryWorker drains a DeliveryQueue and executes each job: resolve the
// destination (unless it's already a known host), then act locally
// (publish a realtime event, or re-enter Commit) or remotely (POST to the
// resolved host).
type DeliveryWorker struct {
	config    *domain.Config
	client    DeliveryClient
	pubsub    PubSub
	committer Committer
	queue     DeliveryQueue
}

func NewDeliveryWorker(
	config *domain.Config,
	client DeliveryClient,
	pubsub PubSub,
	committer Committer,
	queue DeliveryQueue,
) *DeliveryWorker {
	return &DeliveryWorker{
		config:    config,
		client:    client,
		pubsub:    pubsub,
		committer: committer,
		queue:     queue,
	}
}

func (w *DeliveryWorker) Start(ctx context.Context) {
	go func() {
		if err := w.queue.Run(ctx, deliveryConcurrency, w.handle); err != nil {
			slog.Error("delivery worker: queue stopped", slog.String("error", err.Error()))
		}
	}()
}

func (w *DeliveryWorker) handle(ctx context.Context, job domain.DeliveryJob) error {
	host := job.Host
	if job.ResolveURI != "" {
		resolved, err := w.client.ResolveResourceHost(ctx, job.ResolveURI)
		if err != nil {
			return err
		}
		host = resolved
	}

	if host == w.config.FQDN {
		switch job.Local {
		case domain.DeliveryLocalPublish:
			if job.Event == nil {
				return fmt.Errorf("delivery worker: local publish job %s missing event", job.ID)
			}
			return w.pubsub.Publish(ctx, job.ResolveURI, *job.Event)
		case domain.DeliveryLocalCommit:
			_, err := w.committer.Commit(ctx, job.IP, job.Payload, domain.CommitModeExecute)
			return err
		default:
			return nil
		}
	}

	if job.Remote == domain.DeliveryRemoteCommit {
		return w.client.Commit(ctx, host, job.Payload)
	}
	return nil
}
