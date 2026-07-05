package jobqueue_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/infra/jobqueue"
	"github.com/concrnt/concrnt/internal/testutil"
)

func TestRedisDeliveryQueue_EnqueueAndProcess(t *testing.T) {
	rdb, cleanup := testutil.CreateRDB()
	defer cleanup()

	q := jobqueue.NewRedisDeliveryQueue(rdb, jobqueue.WithConsumerName("test-success"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var processed int32
	done := make(chan struct{}, 1)
	go q.Run(ctx, 2, func(ctx context.Context, job domain.DeliveryJob) error {
		atomic.AddInt32(&processed, 1)
		done <- struct{}{}
		return nil
	})

	if err := q.Enqueue(context.Background(), domain.DeliveryJob{Host: "example.com"}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for job to be processed")
	}

	if got := atomic.LoadInt32(&processed); got != 1 {
		t.Fatalf("processed = %d, want 1", got)
	}
}

func TestRedisDeliveryQueue_RetryThenDeadLetter(t *testing.T) {
	rdb, cleanup := testutil.CreateRDB()
	defer cleanup()

	q := jobqueue.NewRedisDeliveryQueue(rdb,
		jobqueue.WithConsumerName("test-dlq"),
		jobqueue.WithMaxAttempts(3),
		jobqueue.WithBaseBackoff(50*time.Millisecond),
		jobqueue.WithBackoffCap(50*time.Millisecond),
		jobqueue.WithMoverInterval(20*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var calls int

	go q.Run(ctx, 1, func(ctx context.Context, job domain.DeliveryJob) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return errors.New("boom")
	})

	if err := q.Enqueue(context.Background(), domain.DeliveryJob{Host: "example.com"}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		c := calls
		mu.Unlock()
		if c >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	finalCalls := calls
	mu.Unlock()
	if finalCalls != 3 {
		t.Fatalf("handler called %d times, want 3", finalCalls)
	}

	// The 3rd failure dead-letters synchronously inside process(); give it a
	// moment to land before asserting on the DLQ.
	time.Sleep(200 * time.Millisecond)

	n, err := q.ReinjectDLQ(context.Background())
	if err != nil {
		t.Fatalf("ReinjectDLQ failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("ReinjectDLQ moved %d jobs, want 1", n)
	}
}
