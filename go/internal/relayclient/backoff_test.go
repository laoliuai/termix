package relayclient

import (
	"testing"
	"time"
)

func TestComputeBackoffSchedule(t *testing.T) {
	rng := func() float64 { return 0.5 }
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 5 * time.Second},
		{3, 10 * time.Second},
		{4, 30 * time.Second},
		{5, 30 * time.Second},
		{50, 30 * time.Second},
	}
	for _, tc := range cases {
		got := computeBackoff(tc.attempt, rng)
		if got != tc.want {
			t.Errorf("attempt=%d got %v want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestComputeBackoffJitterBounds(t *testing.T) {
	min := computeBackoff(2, func() float64 { return 0.0 })
	max := computeBackoff(2, func() float64 { return 1.0 })
	if min != 4*time.Second {
		t.Errorf("min jitter at attempt=2 got %v want 4s", min)
	}
	if max != 6*time.Second {
		t.Errorf("max jitter at attempt=2 got %v want 6s", max)
	}
}
