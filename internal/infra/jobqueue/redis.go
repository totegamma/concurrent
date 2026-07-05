// Package jobqueue provides an at-least-once, retrying delivery queue for
// federation delivery jobs. RedisDeliveryQueue is the current (Redis
// Streams-backed) implementation; usecase/worker code depends only on the
// small interfaces they declare themselves (see internal/usecase.DeliveryQueue
// and internal/worker.DeliveryQueue), so this backend can be swapped later
// without touching either.
package jobqueue

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/concrnt/concrnt/internal/domain"
)

const (
	streamKey    = "delivery:stream"
	retryZSetKey = "delivery:retry"
	dlqStreamKey = "delivery:dlq"
	groupName    = "delivery"

	defaultMaxAttempts     = 8
	defaultBaseBackoff     = 10 * time.Second
	defaultBackoffCap      = time.Hour
	defaultClaimMinIdle    = 5 * time.Minute
	defaultMoverInterval   = time.Second
	defaultReclaimInterval = time.Minute
	defaultStreamMaxLen    = 100_000

	bookkeepingTimeout   = 5 * time.Second
	handlerTimeout       = 30 * time.Second
	groupCreateRetryWait = 2 * time.Second
)

// RedisDeliveryQueue is a Redis Streams-backed delivery queue: a consumer
// group drains streamKey, failures are rescheduled via a delayed-retry ZSET
// (Redis Streams has no native delayed delivery), and jobs that exhaust
// their attempts land in a dead-letter stream for later inspection/replay.
type RedisDeliveryQueue struct {
	rdb *redis.Client

	consumerName    string
	maxAttempts     int
	baseBackoff     time.Duration
	backoffCap      time.Duration
	claimMinIdle    time.Duration
	moverInterval   time.Duration
	reclaimInterval time.Duration
	streamMaxLen    int64
}

type Option func(*RedisDeliveryQueue)

func WithMaxAttempts(n int) Option {
	return func(q *RedisDeliveryQueue) { q.maxAttempts = n }
}

func WithBaseBackoff(d time.Duration) Option {
	return func(q *RedisDeliveryQueue) { q.baseBackoff = d }
}

func WithBackoffCap(d time.Duration) Option {
	return func(q *RedisDeliveryQueue) { q.backoffCap = d }
}

func WithClaimMinIdle(d time.Duration) Option {
	return func(q *RedisDeliveryQueue) { q.claimMinIdle = d }
}

func WithMoverInterval(d time.Duration) Option {
	return func(q *RedisDeliveryQueue) { q.moverInterval = d }
}

func WithReclaimInterval(d time.Duration) Option {
	return func(q *RedisDeliveryQueue) { q.reclaimInterval = d }
}

func WithConsumerName(name string) Option {
	return func(q *RedisDeliveryQueue) { q.consumerName = name }
}

func NewRedisDeliveryQueue(rdb *redis.Client, opts ...Option) *RedisDeliveryQueue {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "worker"
	}

	q := &RedisDeliveryQueue{
		rdb:             rdb,
		consumerName:    hostname,
		maxAttempts:     defaultMaxAttempts,
		baseBackoff:     defaultBaseBackoff,
		backoffCap:      defaultBackoffCap,
		claimMinIdle:    defaultClaimMinIdle,
		moverInterval:   defaultMoverInterval,
		reclaimInterval: defaultReclaimInterval,
		streamMaxLen:    defaultStreamMaxLen,
	}

	for _, opt := range opts {
		opt(q)
	}

	return q
}

// Enqueue adds a job to the stream, filling in ID/CreatedAt if unset.
func (q *RedisDeliveryQueue) Enqueue(ctx context.Context, job domain.DeliveryJob) error {
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	return q.enqueueRaw(ctx, job)
}

func (q *RedisDeliveryQueue) enqueueRaw(ctx context.Context, job domain.DeliveryJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return q.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		MaxLen: q.streamMaxLen,
		Approx: true,
		Values: map[string]interface{}{"payload": string(payload)},
	}).Err()
}

// Run drains the stream with `concurrency` consumer goroutines, plus a
// mover goroutine (moves due retries back onto the stream) and a reclaimer
// goroutine (recovers messages left pending by a crashed consumer). It
// blocks until ctx is cancelled.
func (q *RedisDeliveryQueue) Run(ctx context.Context, concurrency int, handler func(ctx context.Context, job domain.DeliveryJob) error) error {
	if err := q.ensureGroup(ctx); err != nil {
		return err
	}

	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		consumer := q.consumerName + "-" + strconv.Itoa(i)
		wg.Add(1)
		go func(consumer string) {
			defer wg.Done()
			q.consumeLoop(ctx, consumer, handler)
		}(consumer)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		q.moverLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		q.reclaimLoop(ctx, handler)
	}()

	<-ctx.Done()
	wg.Wait()
	return nil
}

// ensureGroup creates the consumer group, retrying on transient errors (e.g.
// Redis not yet reachable at process startup) instead of giving up, so a
// brief Redis outage during boot doesn't permanently disable delivery.
func (q *RedisDeliveryQueue) ensureGroup(ctx context.Context) error {
	for {
		err := q.rdb.XGroupCreateMkStream(ctx, streamKey, groupName, "0").Err()
		if err == nil || strings.Contains(err.Error(), "BUSYGROUP") {
			return nil
		}
		slog.Error("delivery queue: failed to create consumer group, retrying", slog.String("error", err.Error()))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(groupCreateRetryWait):
		}
	}
}

func (q *RedisDeliveryQueue) consumeLoop(ctx context.Context, consumer string, handler func(context.Context, domain.DeliveryJob) error) {
	for ctx.Err() == nil {
		streams, err := q.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    groupName,
			Consumer: consumer,
			Streams:  []string{streamKey, ">"},
			Count:    1,
			Block:    5 * time.Second,
		}).Result()
		if err != nil {
			if ctx.Err() != nil || err == redis.Nil {
				continue
			}
			slog.Error("delivery queue: XReadGroup failed", slog.String("error", err.Error()))
			time.Sleep(time.Second)
			continue
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				q.process(ctx, msg, handler)
			}
		}
	}
}

func (q *RedisDeliveryQueue) reclaimLoop(ctx context.Context, handler func(context.Context, domain.DeliveryJob) error) {
	ticker := time.NewTicker(q.reclaimInterval)
	defer ticker.Stop()
	consumer := q.consumerName + "-reclaimer"

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			messages, _, err := q.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream:   streamKey,
				Group:    groupName,
				Consumer: consumer,
				MinIdle:  q.claimMinIdle,
				Start:    "0",
				Count:    100,
			}).Result()
			if err != nil {
				if ctx.Err() == nil {
					slog.Error("delivery queue: XAutoClaim failed", slog.String("error", err.Error()))
				}
				continue
			}
			for _, msg := range messages {
				q.process(ctx, msg, handler)
			}
		}
	}
}

func (q *RedisDeliveryQueue) moverLoop(ctx context.Context) {
	ticker := time.NewTicker(q.moverInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			q.moveDueRetries(ctx)
		}
	}
}

func (q *RedisDeliveryQueue) moveDueRetries(ctx context.Context) {
	now := strconv.FormatInt(time.Now().UnixMilli(), 10)
	members, err := q.rdb.ZRangeByScore(ctx, retryZSetKey, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   now,
		Count: 100,
	}).Result()
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("delivery queue: ZRangeByScore failed", slog.String("error", err.Error()))
		}
		return
	}

	for _, member := range members {
		if err := q.rdb.XAdd(ctx, &redis.XAddArgs{
			Stream: streamKey,
			MaxLen: q.streamMaxLen,
			Approx: true,
			Values: map[string]interface{}{"payload": member},
		}).Err(); err != nil {
			slog.Error("delivery queue: failed to re-enqueue due retry", slog.String("error", err.Error()))
			continue
		}
		// A crash between XAdd and ZRem duplicates the job rather than
		// losing it, which is acceptable under at-least-once delivery.
		q.rdb.ZRem(ctx, retryZSetKey, member)
	}
}

// process handles a single claimed stream message: run the handler, then
// ack+delete it from the stream and either drop it (success), reschedule it
// (retryable failure), or dead-letter it (attempts exhausted). Bookkeeping
// writes use their own short-lived context so a cancelled loop ctx can't
// lose accounting for a job that already ran.
func (q *RedisDeliveryQueue) process(ctx context.Context, msg redis.XMessage, handler func(context.Context, domain.DeliveryJob) error) {
	raw, _ := msg.Values["payload"].(string)

	var job domain.DeliveryJob
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		slog.Error("delivery queue: dropping unparseable job", slog.String("streamId", msg.ID), slog.String("error", err.Error()))
		q.deadLetterRaw(raw)
		q.ackAndDel(msg.ID)
		return
	}

	handlerCtx, cancel := context.WithTimeout(ctx, handlerTimeout)
	err := handler(handlerCtx, job)
	cancel()

	if err == nil {
		q.ackAndDel(msg.ID)
		return
	}

	if ctx.Err() != nil {
		// The loop is shutting down: leave the message unacked/pending
		// rather than counting this as a genuine attempt. It will be
		// picked back up (via XAutoClaim) once claimMinIdle elapses,
		// with its retry budget intact.
		return
	}

	job.Attempt++
	job.LastError = err.Error()

	if job.Attempt >= q.maxAttempts {
		slog.Error("delivery queue: exhausted retries, moving to DLQ",
			slog.String("jobId", job.ID), slog.Int("attempt", job.Attempt), slog.String("error", err.Error()))
		q.deadLetterJob(job)
		q.ackAndDel(msg.ID)
		return
	}

	q.scheduleRetry(job)
	q.ackAndDel(msg.ID)
}

func (q *RedisDeliveryQueue) ackAndDel(streamID string) {
	ctx, cancel := context.WithTimeout(context.Background(), bookkeepingTimeout)
	defer cancel()
	if err := q.rdb.XAck(ctx, streamKey, groupName, streamID).Err(); err != nil {
		slog.Error("delivery queue: XAck failed", slog.String("streamId", streamID), slog.String("error", err.Error()))
	}
	q.rdb.XDel(ctx, streamKey, streamID)
}

func (q *RedisDeliveryQueue) scheduleRetry(job domain.DeliveryJob) {
	payload, err := json.Marshal(job)
	if err != nil {
		slog.Error("delivery queue: failed to marshal job for retry", slog.String("error", err.Error()))
		return
	}

	nextAttempt := time.Now().Add(backoffDuration(job.Attempt, q.baseBackoff, q.backoffCap))

	ctx, cancel := context.WithTimeout(context.Background(), bookkeepingTimeout)
	defer cancel()
	if err := q.rdb.ZAdd(ctx, retryZSetKey, redis.Z{
		Score:  float64(nextAttempt.UnixMilli()),
		Member: string(payload),
	}).Err(); err != nil {
		slog.Error("delivery queue: failed to schedule retry", slog.String("error", err.Error()))
	}
}

func (q *RedisDeliveryQueue) deadLetterJob(job domain.DeliveryJob) {
	payload, err := json.Marshal(job)
	if err != nil {
		slog.Error("delivery queue: failed to marshal job for DLQ", slog.String("error", err.Error()))
		return
	}
	q.deadLetterRaw(string(payload))
}

func (q *RedisDeliveryQueue) deadLetterRaw(payload string) {
	ctx, cancel := context.WithTimeout(context.Background(), bookkeepingTimeout)
	defer cancel()
	if err := q.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: dlqStreamKey,
		MaxLen: q.streamMaxLen,
		Approx: true,
		Values: map[string]interface{}{"payload": payload},
	}).Err(); err != nil {
		slog.Error("delivery queue: failed to write to DLQ", slog.String("error", err.Error()))
	}
}

// ReinjectDLQ re-enqueues every job currently in the dead-letter stream
// (attempt counter and last error reset) and removes it from the DLQ.
func (q *RedisDeliveryQueue) ReinjectDLQ(ctx context.Context) (int, error) {
	entries, err := q.rdb.XRange(ctx, dlqStreamKey, "-", "+").Result()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		raw, _ := entry.Values["payload"].(string)

		var job domain.DeliveryJob
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			slog.Error("delivery queue: failed to unmarshal DLQ entry, skipping", slog.String("streamId", entry.ID), slog.String("error", err.Error()))
			continue
		}
		job.Attempt = 0
		job.LastError = ""

		if err := q.enqueueRaw(ctx, job); err != nil {
			return count, err
		}
		if err := q.rdb.XDel(ctx, dlqStreamKey, entry.ID).Err(); err != nil {
			slog.Error("delivery queue: failed to delete reinjected DLQ entry", slog.String("streamId", entry.ID), slog.String("error", err.Error()))
		}
		count++
	}
	return count, nil
}

// backoffDuration returns base*3^(attempt-1), capped at capD.
func backoffDuration(attempt int, base, capD time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := base
	for i := 1; i < attempt; i++ {
		if d >= capD {
			return capD
		}
		d *= 3
	}
	if d > capD {
		return capD
	}
	return d
}
