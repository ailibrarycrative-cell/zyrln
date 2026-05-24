package tunnel

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConnectHost(t *testing.T) {
	if got := connectHost("www.google.com:443"); got != "www.google.com" {
		t.Fatalf("connectHost = %q", got)
	}
	if got := connectHost("example.com"); got != "example.com" {
		t.Fatalf("connectHost bare = %q", got)
	}
}

func TestHandleTunnelHTTP_RequiresCONNECT(t *testing.T) {
	tc := NewTunnelClient(http.DefaultClient, []string{"https://example.com"}, "", "k", time.Second)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "http://example.com/test", nil)
	handleTunnelHTTP(w, r, &tunnelBundle{tunnel: tc})

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", w.Code)
	}
}
