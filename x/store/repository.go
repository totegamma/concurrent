package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/totegamma/concurrent/core"
)

type Repository interface {
	Log(ctx context.Context, commit core.CommitLog) (core.CommitLog, error)
	GetArchiveByOwner(ctx context.Context, owner string) (string, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db}
}

func (r *repository) Log(ctx context.Context, commit core.CommitLog) (core.CommitLog, error) {
	ctx, span := tracer.Start(ctx, "Store.Repository.Log")
	defer span.End()

	err := r.db.WithContext(ctx).Create(&commit).Error
	return commit, err
}

func (r *repository) GetByOwner(ctx context.Context, owner string) ([]core.CommitLog, error) {
	ctx, span := tracer.Start(ctx, "Store.Repository.GetByOwner")
	defer span.End()

	var commits []core.CommitLog
	err := r.db.WithContext(ctx).Where("ANY(owners) = ?", owner).Find(&commits).Error
	return commits, err
}

func (r *repository) GetArchiveByOwner(ctx context.Context, owner string) (string, error) {
	ctx, span := tracer.Start(ctx, "Store.Repository.GetLogsByOwner")
	defer span.End()

	var commits []core.CommitLog
	err := r.db.
		WithContext(ctx).
		Where("? = ANY(owners)", owner).
		Where("is_ephemeral = ?", false).
		Order("signed_at ASC").
		Find(&commits).
		Error
	if err != nil {
		return "", err
	}

	var logs string
	for _, commit := range commits {
		// ID Owner Signature Document
		logs += fmt.Sprintf("%s %s %s %s\n", commit.DocumentID, owner, commit.Signature, commit.Document)
	}

	return logs, nil
}
