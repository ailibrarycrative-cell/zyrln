package tunnel

import (
	"testing"
	"time"
)

func TestTunnelRXWaitMS(t *testing.T) {
	if got := tunnelRXWaitMS(true, 200*time.Millisecond); got != 1 {
		t.Fatalf("with TX: got %d ms, want 1", got)
	}
	if got := tunnelRXWaitMS(false, 10*time.Millisecond); got != 30 {
		t.Fatalf("idle clamp min: got %d ms, want 30", got)
	}
	if got := tunnelRXWaitMS(false, 500*time.Millisecond); got != 300 {
		t.Fatalf("idle clamp max: got %d ms, want 300", got)
	}
}
