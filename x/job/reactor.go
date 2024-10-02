package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/totegamma/concurrent/core"
)

type reactor struct {
	store core.StoreService
	job   core.JobService
}

type Reactor interface {
	Start(ctx context.Context)
}

// Newreactor creates a new reactor
func NewReactor(
	store core.StoreService,
	job core.JobService,
) Reactor {
	return &reactor{
		store,
		job,
	}
}

// Boot starts reactor
func (r *reactor) Start(ctx context.Context) {
	slog.Info("reactor start!")

	ticker60 := time.NewTicker(60 * time.Second)
	go func() {
		for {
			select {
			case <-ticker60.C:
				ctx, span := tracer.Start(ctx, "reactor.Boot.DispatchJobs")
				r.dispatchJobs(ctx)
				span.End()
				break
			}
		}
	}()
}

func (a *reactor) dispatchJobs(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "reactor.DispatchJobs")
	defer span.End()

	job, err := a.job.Dequeue(ctx)
	if err != nil {
		return
	}

	switch job.Type {
	case "clean":
		go a.dispatchJob(ctx, job, a.jobClean)
	case "hello":
		go a.dispatchJob(ctx, job, a.JobHello)
	default:
		slog.ErrorContext(ctx, "unknown job type",
			slog.String("type", job.Type),
		)
		a.job.Complete(ctx, job.ID, "failed", "unknown job type")
	}
}

func (a *reactor) dispatchJob(ctx context.Context, job *core.Job, fn func(context.Context, *core.Job) (string, error)) {
	ctx, span := tracer.Start(ctx, "reactor.DispatchJob")
	defer span.End()

	result, err := fn(ctx, job)
	if err != nil {
		slog.ErrorContext(ctx, "failed to process job", slog.String("error", err.Error()))

		_, err = a.job.Complete(ctx, job.ID, "failed: "+result, err.Error())
		if err != nil {
			span.RecordError(err)
			slog.ErrorContext(ctx, "failed to complete job", slog.String("error", err.Error()))
		}
	}

	_, err = a.job.Complete(ctx, job.ID, "completed", result)
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(ctx, "failed to complete job", slog.String("error", err.Error()))
	}
}

func (a *reactor) jobClean(ctx context.Context, job *core.Job) (string, error) {
	ctx, span := tracer.Start(ctx, "reactor.JobClean")
	defer span.End()

	return "", a.store.CleanUserAllData(ctx, job.Author)
}

func (a *reactor) JobHello(ctx context.Context, job *core.Job) (string, error) {
	ctx, span := tracer.Start(ctx, "reactor.JobHello")
	defer span.End()

	return "hello!", nil
}
