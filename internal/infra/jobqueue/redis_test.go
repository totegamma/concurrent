package jobqueue_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/concrnt/concrnt/internal/infra/jobqueue"
	"github.com/concrnt/concrnt/internal/testutil"
)

type testPayload struct {
	Host string `json:"host"`
}

func TestRedisJobQueue_EnqueueAndProcess(t *testing.T) {
	rdb, cleanup := testutil.CreateRDB()
	defer cleanup()

	q := jobqueue.NewRedisJobQueue(rdb, jobqueue.WithConsumerName("test-success"), jobqueue.WithConcurrency(2))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan testPayload, 1)
	q.RegisterHandler("test", func(ctx context.Context, payload json.RawMessage) error {
		var p testPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		received <- p
		return nil
	})
	go q.Run(ctx)

	if err := q.Enqueue(context.Background(), "test", testPayload{Host: "example.com"}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	select {
	case p := <-received:
		if p.Host != "example.com" {
			t.Fatalf("handler received payload %+v, want the enqueued one", p)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for job to be processed")
	}
	select {
	case p := <-received:
		t.Fatalf("job processed twice: %+v", p)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestRedisJobQueue_RetryThenDeadLetter(t *testing.T) {
	rdb, cleanup := testutil.CreateRDB()
	defer cleanup()

	q := jobqueue.NewRedisJobQueue(rdb,
		jobqueue.WithConsumerName("test-dlq"),
		jobqueue.WithConcurrency(1),
		jobqueue.WithMaxAttempts(3),
		jobqueue.WithBaseBackoff(50*time.Millisecond),
		jobqueue.WithBackoffCap(50*time.Millisecond),
		jobqueue.WithMoverInterval(20*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var calls int

	q.RegisterHandler("test", func(ctx context.Context, payload json.RawMessage) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return errors.New("boom")
	})
	go q.Run(ctx)

	if err := q.Enqueue(context.Background(), "test", testPayload{Host: "example.com"}); err != nil {
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

// A job type this consumer has no handler for is retried like any failure
// (another replica may own the handler mid-rollout), not dropped or
// dead-lettered on first sight; it reaches the DLQ only once the retry
// budget is exhausted.
func TestRedisJobQueue_UnknownTypeIsRetried(t *testing.T) {
	rdb, cleanup := testutil.CreateRDB()
	defer cleanup()

	q := jobqueue.NewRedisJobQueue(rdb,
		jobqueue.WithConsumerName("test-unknown"),
		jobqueue.WithConcurrency(1),
		jobqueue.WithMaxAttempts(2),
		jobqueue.WithBaseBackoff(50*time.Millisecond),
		jobqueue.WithBackoffCap(50*time.Millisecond),
		jobqueue.WithMoverInterval(20*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go q.Run(ctx)

	if err := q.Enqueue(context.Background(), "unknown", testPayload{Host: "example.com"}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// first attempt fails immediately, the retry is due after the backoff;
	// well before that the job must not be in the DLQ yet
	time.Sleep(20 * time.Millisecond)
	if n, err := q.ReinjectDLQ(context.Background()); err != nil || n != 0 {
		t.Fatalf("job dead-lettered on first sight (reinjected %d, err %v), want retry", n, err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		n, err := q.ReinjectDLQ(context.Background())
		if err != nil {
			t.Fatalf("ReinjectDLQ failed: %v", err)
		}
		if n == 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("job never reached the DLQ after exhausting retries")
}
