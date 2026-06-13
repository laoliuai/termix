package relayclient

import "testing"

// TestGenerationTrackingPerSession verifies the per-session counter increments
// independently and currentGeneration does not advance it.
func TestGenerationTrackingPerSession(t *testing.T) {
	c := New("ws://dummy", "token", "device")

	if got := c.nextGeneration("s1"); got != 1 {
		t.Fatalf("s1 first nextGeneration = %d, want 1", got)
	}
	if got := c.nextGeneration("s2"); got != 1 {
		t.Fatalf("s2 first nextGeneration = %d, want 1", got)
	}
	if got := c.nextGeneration("s1"); got != 2 {
		t.Fatalf("s1 second nextGeneration = %d, want 2", got)
	}
	if got := c.currentGeneration("s1"); got != 2 {
		t.Fatalf("currentGeneration(s1) = %d, want 2 (no increment)", got)
	}
	if got := c.currentGeneration("s1"); got != 2 {
		t.Fatalf("currentGeneration(s1) second read = %d, want 2", got)
	}
	if got := c.currentGeneration("s2"); got != 1 {
		t.Fatalf("currentGeneration(s2) = %d, want 1", got)
	}
}
