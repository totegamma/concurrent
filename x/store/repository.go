package store

import (
	"context"
	"fmt"
	"time"

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

	var logs string
	var lastSignedAt time.Time

	var pageSize = 10

	for {
		var commits []core.CommitLog
		err := r.db.
			WithContext(ctx).
			Where("? = ANY(owners)", owner).
			Where("is_ephemeral = ?", false).
			Where("signed_at > ?", lastSignedAt).
			Order("signed_at ASC").
			Find(&commits).
			Limit(pageSize).
			Error

		if err != nil {
			span.RecordError(err)
			return "", err
		}

		for _, commit := range commits {
			// ID Owner Signature Document
			logs += fmt.Sprintf("%s %s %s %s\n", commit.DocumentID, owner, commit.Signature, commit.Document)
		}

		if len(commits) > 0 {
			lastSignedAt = commits[len(commits)-1].SignedAt
		}

		if len(commits) < pageSize {
			break
		}
	}

	return logs, nil
}
