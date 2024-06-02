package agent

import (
	"context"
	"log/slog"

	"github.com/totegamma/concurrent/core"
)

func (a *agent) dispatchJobs(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "Agent.DispatchJobs")
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

func (a *agent) dispatchJob(ctx context.Context, job *core.Job, fn func(context.Context, *core.Job) (string, error)) {
	ctx, span := tracer.Start(ctx, "Agent.DispatchJob")
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

func (a *agent) jobClean(ctx context.Context, job *core.Job) (string, error) {
	ctx, span := tracer.Start(ctx, "Agent.JobClean")
	defer span.End()

	return "", a.store.CleanUserAllData(ctx, job.Author)
}

func (a *agent) JobHello(ctx context.Context, job *core.Job) (string, error) {
	ctx, span := tracer.Start(ctx, "Agent.JobHello")
	defer span.End()

	return "hello!", nil
}
