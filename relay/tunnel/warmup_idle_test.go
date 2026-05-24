package tunnel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeepalive_SkippedWhileBridgeActive(t *testing.T) {
	var pings atomic.Int32
	apps := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pings.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer apps.Close()

	client := NewTunnelClient(apps.Client(), []string{apps.URL}, testFrontDomain(apps), "k", 5*time.Second)
	defer client.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = client.WaitWarmup(ctx)
	cancel()
	pings.Store(0)

	client.beginBridge()
	client.keepaliveTick(false)
	client.keepaliveTick(true)
	client.endBridge()

	if got := pings.Load(); got != 0 {
		t.Fatalf("keepalive while busy: %d pings, want 0", got)
	}

	client.lastTraffic.Store(time.Now().Add(-tunnelUserIdleGrace - time.Second).UnixNano())
	client.keepaliveTick(false)
	if pings.Load() == 0 {
		t.Fatal("expected keepalive after idle")
	}
}

func TestWarmup_AbortsWhenUserStarts(t *testing.T) {
	block := make(chan struct{})
	var pingStarted atomic.Bool
	apps := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !pingStarted.CompareAndSwap(false, true) {
			<-block
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer apps.Close()
	defer close(block)

	c := newPrewarmClient(apps.Client(), []string{apps.URL}, testFrontDomain(apps), "k", 5*time.Second)
	defer c.Stop()

	deadline := time.After(2 * time.Second)
	for !pingStarted.Load() {
		select {
		case <-deadline:
			t.Fatal("warmup never started")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	c.beginBridge()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.WaitWarmup(ctx); err != nil {
		t.Fatalf("WaitWarmup: %v", err)
	}
	if !c.WarmReady() {
		t.Fatal("expected warm ready after abort")
	}
}

func TestPrewarmClient_DeferredKeepalive(t *testing.T) {
	apps := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer apps.Close()

	c := newPrewarmClient(apps.Client(), []string{apps.URL}, testFrontDomain(apps), "k", 5*time.Second)
	if c.deferKeepalive {
		c.Activate()
	}
	c.Stop()
}
