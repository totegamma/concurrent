package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/testutil"
)

func TestSignalRealtimeAcknowledgesSubscribedAfterRedisSubscribe(t *testing.T) {
	rdb, cleanup := testutil.CreateRDB()
	defer cleanup()

	signal := NewSignalService(rdb)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	requests := make(chan []string)
	responses := make(chan concrnt.Event)
	go signal.Realtime(ctx, requests, responses)

	prefix := "cckv://remote.example/concrnt.world/timeline"
	requests <- []string{prefix}

	var ack concrnt.Event
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		select {
		case ack = <-responses:
			require.Equal(c, "subscribed", ack.Type)
		default:
			require.Fail(c, "subscription ack was not received")
		}
	}, 5*time.Second, 10*time.Millisecond)

	require.Equal(t, []string{prefix + "*"}, ack.Prefixes)
	require.Eventually(t, func() bool {
		return len(signal.GetCurrentSubscriptions()) == 1 && signal.GetCurrentSubscriptions()[0] == prefix+"*"
	}, 5*time.Second, 10*time.Millisecond)
}

func TestSignalRealtimeStopsWhenRequestChannelCloses(t *testing.T) {
	rdb, cleanup := testutil.CreateRDB()
	defer cleanup()

	signal := NewSignalService(rdb)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	requests := make(chan []string)
	events := make(chan concrnt.Event, 1)
	done := make(chan struct{})
	go func() {
		signal.Realtime(ctx, requests, events)
		close(done)
	}()

	close(requests)

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}
