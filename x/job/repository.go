package job

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/totegamma/concurrent/core"
)

type Repository interface {
	List(ctx context.Context, authorID string) ([]core.Job, error)
	Enqueue(ctx context.Context, author, typ, payload string, scheduled time.Time) (core.Job, error)
	Dequeue(ctx context.Context) (*core.Job, error)
	Complete(ctx context.Context, id, status, result string) (core.Job, error)
	Cancel(ctx context.Context, id string) (core.Job, error)
	Clean(ctx context.Context, olderThan time.Time) ([]core.Job, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db}
}

func (r *repository) List(ctx context.Context, authorID string) ([]core.Job, error) {
	ctx, span := tracer.Start(ctx, "Job.Repository.List")
	defer span.End()

	var jobs []core.Job
	err := r.db.Where("author = ?", authorID).Find(&jobs).Error
	if err != nil {
		return nil, err
	}

	return jobs, nil
}

func (r *repository) Enqueue(ctx context.Context, author, typ, payload string, scheduled time.Time) (core.Job, error) {
	ctx, span := tracer.Start(ctx, "Job.Repository.Enqueue")
	defer span.End()

	job := core.Job{
		Author:    author,
		Type:      typ,
		Payload:   payload,
		Scheduled: scheduled,
		Status:    "pending",
	}

	if err := r.db.WithContext(ctx).Create(&job).Error; err != nil {
		return core.Job{}, err
	}

	return job, nil
}

func (r *repository) Dequeue(ctx context.Context) (*core.Job, error) {
	ctx, span := tracer.Start(ctx, "Job.Repository.Dequeue")
	defer span.End()

	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		span.RecordError(tx.Error)
		return nil, tx.Error
	}

	var job core.Job
	err := tx.WithContext(ctx).
		Model(&core.Job{}).
		Where("status = 'pending' AND scheduled <= ?", time.Now()).
		Order("scheduled ASC").
		First(&job).Error

	if err != nil {
		span.RecordError(err)
		tx.Rollback()
		return nil, err
	}

	job.Status = "running"
	job.TraceID = span.SpanContext().TraceID().String()
	if tx.WithContext(ctx).Save(&job).Error != nil {
		span.RecordError(err)
		tx.Rollback()
		return nil, err
	}

	tx.WithContext(ctx).Commit()

	return &job, nil
}

func (r *repository) Complete(ctx context.Context, id, status, result string) (core.Job, error) {
	ctx, span := tracer.Start(ctx, "Job.Repository.Complete")
	defer span.End()

	var job core.Job
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&job).Error
	if err != nil {
		return core.Job{}, err
	}

	job.Status = status
	job.Result = result

	if err := r.db.WithContext(ctx).Save(&job).Error; err != nil {
		return core.Job{}, err
	}

	return job, nil
}

func (r *repository) Cancel(ctx context.Context, id string) (core.Job, error) {
	ctx, span := tracer.Start(ctx, "Job.Repository.Cancel")
	defer span.End()

	var job core.Job
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&job).Error
	if err != nil {
		return core.Job{}, err
	}

	job.Status = "canceled"

	if err := r.db.WithContext(ctx).Save(&job).Error; err != nil {
		return core.Job{}, err
	}

	return job, nil
}

func (r *repository) Clean(ctx context.Context, olderThan time.Time) ([]core.Job, error) {
	ctx, span := tracer.Start(ctx, "Job.Repository.Clean")
	defer span.End()

	var jobs []core.Job
	err := r.db.WithContext(ctx).Where("scheduled < ? AND (status = 'completed' OR status = 'failed' OR status = 'canceled')", olderThan).Find(&jobs).Error
	if err != nil {
		return nil, err
	}

	return jobs, nil
}
