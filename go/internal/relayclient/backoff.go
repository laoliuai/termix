package relayclient

import "time"

// backoffSchedule is the canonical reconnect delay sequence. Past the
// last entry the supervisor stays at the cap (last entry).
var backoffSchedule = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

// computeBackoff returns the delay before the supervisor's next reconnect
// attempt. Applies ±20% jitter using the supplied rng (which must return
// a value in [0, 1]) to spread reconnects from many daemons recovering
// after a relay restart.
func computeBackoff(attempt int, rng func() float64) time.Duration {
	var base time.Duration
	if attempt < len(backoffSchedule) {
		base = backoffSchedule[attempt]
	} else {
		base = backoffSchedule[len(backoffSchedule)-1]
	}
	factor := 0.8 + 0.4*rng()
	return time.Duration(float64(base) * factor)
}
