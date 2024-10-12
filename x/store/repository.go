package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/core"
)

type Repository interface {
	Log(ctx context.Context, commit core.CommitLog) (core.CommitLog, error)
	SyncCommitFile(ctx context.Context, owner string) error
	SyncStatus(ctx context.Context, owner string) (core.SyncStatus, error)
}

type repository struct {
	db  *gorm.DB
	rdb *redis.Client
}

func NewRepository(db *gorm.DB, rdb *redis.Client) Repository {
	return &repository{db, rdb}
}

func (r *repository) Log(ctx context.Context, commit core.CommitLog) (core.CommitLog, error) {
	ctx, span := tracer.Start(ctx, "Store.Repository.Log")
	defer span.End()

	err := r.db.WithContext(ctx).Create(&commit).Error
	return commit, err
}

func (r *repository) getLatestCommitDateByOwner(ctx context.Context, owner string) (time.Time, error) {
	ctx, span := tracer.Start(ctx, "Store.Repository.GetLatestCommitByOwner")
	defer span.End()

	userlogPath := filepath.Join("/tmp/concrnt", "/user")
	err := os.MkdirAll(userlogPath, 0755)
	if err != nil {
		slog.Error("failed to create repository directory:", slog.String("error", err.Error()))
		panic(err)
	}

	filename := fmt.Sprintf("%s.log", owner)
	userStore, err := os.OpenFile(filepath.Join(userlogPath, filename), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		slog.Error("failed to open user log file:", slog.String("error", err.Error()))
		return time.Time{}, err
	}
	defer userStore.Close()

	// find last log entry
	stats, err := userStore.Stat()
	if err != nil {
		slog.Error("failed to stat repository log file:", slog.String("error", err.Error()))
		panic(err)
	}

	var lastLine string
	var seeker int64 = stats.Size()

	for {
		from := seeker - 1024
		to := seeker

		if from < 0 {
			from = 0
		}

		if from == 0 && to == 0 {
			break
		}

		buf := make([]byte, to-from)
		_, err := userStore.ReadAt(buf, from)
		if err != nil {
			slog.Error("failed to read repository log file:", slog.String("error", err.Error()))
			panic(err)
		}

		// remove trailing newline
		if buf[len(buf)-1] == '\n' {
			buf = buf[:len(buf)-1]
		}

		lines := strings.Split(string(buf), "\n")
		if len(lines) > 1 {
			lastLine = lines[len(lines)-1] + lastLine
			break
		}

		lastLine = string(buf) + lastLine

		seeker = from
	}

	split := strings.Split(lastLine, " ")
	if len(split) < 4 {
		return time.Time{}, nil
	}

	document := strings.Join(split[3:], " ")
	object := core.DocumentBase[any]{}
	err = json.Unmarshal([]byte(document), &object)
	if err != nil {
		span.RecordError(err)
		return time.Time{}, errors.Wrap(err, "failed to unmarshal payload")
	}

	return object.SignedAt, nil
}

func (r *repository) SyncStatus(ctx context.Context, owner string) (core.SyncStatus, error) {
	ctx, span := tracer.Start(ctx, "Store.Repository.SyncStatus")
	defer span.End()

	lockKey := fmt.Sprintf("store:lock:%s", owner)
	value, err := r.rdb.Get(ctx, lockKey).Result()
	if err == nil && value != "" {
		return core.SyncStatus{Owner: owner, Status: "syncing"}, nil
	}

	lastSignedAt, err := r.getLatestCommitDateByOwner(ctx, owner)
	if err != nil {
		span.RecordError(err)
		return core.SyncStatus{}, err
	}

	var latestSignedAt time.Time
	err = r.db.
		WithContext(ctx).
		Model(&core.CommitLog{}).
		Where("? = ANY(owners)", owner).
		Where("is_ephemeral = ?", false).
		Order("signed_at DESC").
		Limit(1).
		Pluck("signed_at", &latestSignedAt).
		Error

	if err != nil {
		span.RecordError(err)
		return core.SyncStatus{}, err
	}

	if latestSignedAt.Equal(lastSignedAt) {
		return core.SyncStatus{Owner: owner, Status: "insync", LatestOnFile: lastSignedAt, LatestOnDB: latestSignedAt}, nil
	}

	return core.SyncStatus{Owner: owner, Status: "outofsync", LatestOnFile: lastSignedAt, LatestOnDB: latestSignedAt}, nil
}

func (r *repository) SyncCommitFile(ctx context.Context, owner string) error {
	ctx, span := tracer.Start(ctx, "Store.Repository.GetLogsByOwner")
	defer span.End()

	fmt.Println("SyncCommitFile start")
	defer fmt.Println("SyncCommitFile end")

	// accuire lock
	lockKey := fmt.Sprintf("store:lock:%s", owner)
	_, err := r.rdb.SetNX(ctx, lockKey, "1", 10*time.Minute).Result()
	if err != nil && err != redis.Nil {
		span.RecordError(err)
		return err
	}
	defer r.rdb.Del(ctx, lockKey)

	var logs string
	lastSignedAt, err := r.getLatestCommitDateByOwner(ctx, owner)
	if err != nil {
		span.RecordError(err)
		return err
	}

	var pageSize = 10

	for {
		fmt.Printf("dump lastSignedAt: %v\n", lastSignedAt)
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
			return err
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

	userlogPath := filepath.Join("/tmp/concrnt", "/user")
	err = os.MkdirAll(userlogPath, 0755)
	if err != nil {
		slog.Error("failed to create repository directory:", slog.String("error", err.Error()))
		panic(err)
	}

	filename := fmt.Sprintf("%s.log", owner)
	userStore, err := os.OpenFile(filepath.Join(userlogPath, filename), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		slog.Error("failed to open user log file:", slog.String("error", err.Error()))
		return err
	}
	defer userStore.Close()

	_, err = userStore.WriteString(logs)
	if err != nil {
		slog.Error("failed to write to user log file:", slog.String("error", err.Error()))
		return err
	}

	return nil
}
