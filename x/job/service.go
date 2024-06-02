package job

import (
	"context"
	"time"

	//"github.com/pkg/errors"

	"github.com/totegamma/concurrent/core"
)

type service struct {
	repo Repository
}

func NewService(repo Repository) core.JobService {
	return &service{
		repo,
	}
}

func (s *service) List(ctx context.Context, requester string) ([]core.Job, error) {
	ctx, span := tracer.Start(ctx, "Job.Service.List")
	defer span.End()

	jobs, err := s.repo.List(ctx, requester)
	if err != nil {
		return nil, err
	}

	return jobs, nil
}

func (s *service) Create(ctx context.Context, requester, typ, payload string, scheduled time.Time) (core.Job, error) {
	ctx, span := tracer.Start(ctx, "Job.Service.Create")
	defer span.End()

	/*
	   switch typ {
	   case "cleanup":
	   default:
	       return core.Job{}, errors.New("invalid job type")
	   }
	*/

	job, err := s.repo.Enqueue(ctx, requester, typ, payload, scheduled)
	if err != nil {
		return core.Job{}, err
	}

	return job, nil
}

func (s *service) Dequeue(ctx context.Context) (*core.Job, error) {
	ctx, span := tracer.Start(ctx, "Job.Service.Dequeue")
	defer span.End()

	job, err := s.repo.Dequeue(ctx)
	if err != nil {
		return nil, err
	}

	return job, nil
}

func (s *service) Complete(ctx context.Context, id, status, result string) (core.Job, error) {
	ctx, span := tracer.Start(ctx, "Job.Service.Complete")
	defer span.End()

	job, err := s.repo.Complete(ctx, id, status, result)
	if err != nil {
		return core.Job{}, err
	}

	return job, nil
}

func (s *service) Cancel(ctx context.Context, id string) (core.Job, error) {
	ctx, span := tracer.Start(ctx, "Job.Service.Cancel")
	defer span.End()

	job, err := s.repo.Cancel(ctx, id)
	if err != nil {
		return core.Job{}, err
	}

	return job, nil
}
