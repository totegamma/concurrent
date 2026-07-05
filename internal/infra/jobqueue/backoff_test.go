package jobqueue

import (
	"testing"
	"time"
)

func TestBackoffDuration(t *testing.T) {
	base := 10 * time.Second
	cap := time.Hour

	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 10 * time.Second}, // clamps to attempt=1
		{1, 10 * time.Second},
		{2, 30 * time.Second},
		{3, 90 * time.Second},
		{4, 270 * time.Second},
		{8, cap}, // 10s*3^7 ~= 6h07m, capped to 1h
	}

	for _, c := range cases {
		got := backoffDuration(c.attempt, base, cap)
		if got != c.want {
			t.Errorf("backoffDuration(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestBackoffDurationNeverExceedsCap(t *testing.T) {
	base := 10 * time.Second
	cap := time.Hour
	for attempt := 1; attempt <= 20; attempt++ {
		got := backoffDuration(attempt, base, cap)
		if got > cap {
			t.Fatalf("attempt %d: backoff %v exceeds cap %v", attempt, got, cap)
		}
	}
}
