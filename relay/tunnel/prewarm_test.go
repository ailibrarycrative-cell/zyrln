package tunnel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitWarmup_Completes(t *testing.T) {
	var calls atomic.Int32
	apps := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer apps.Close()

	client := NewTunnelClient(apps.Client(), []string{apps.URL}, testFrontDomain(apps), "k", 5*time.Second)
	defer client.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.WaitWarmup(ctx); err != nil {
		t.Fatalf("WaitWarmup: %v", err)
	}
	if !client.WarmReady() {
		t.Fatal("expected warm ready")
	}
	if calls.Load() == 0 {
		t.Fatal("expected warmup traffic")
	}
}

func TestPrewarm_AdoptedByProxy(t *testing.T) {
	apps := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer apps.Close()

	urls := []string{apps.URL}
	preClient := apps.Client()
	postClient := &http.Client{Transport: apps.Client().Transport, Timeout: 5 * time.Second}
	Prewarm(preClient, urls, testFrontDomain(apps), "k", 5*time.Second)

	adopted := adoptOrCreateTunnelClient(postClient, urls, testFrontDomain(apps), "k", 5*time.Second)
	if adopted.httpClient() != postClient {
		t.Fatal("expected adopted client to use proxy HTTP client")
	}
	defer adopted.Stop()

	prewarmMu.Lock()
	still := prewarmClient
	prewarmMu.Unlock()
	if still != nil {
		t.Fatal("expected prewarm client to be adopted")
	}
	if adopted == nil {
		t.Fatal("expected adopted client")
	}
}
