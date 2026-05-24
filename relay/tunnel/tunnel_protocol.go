package tunnel

import (
	"net"
	"strings"
	"time"
)

const (
	// Sized for Apps Script round-trip cost (~1–2s): fewer, larger frames beat many small ones.
	TunnelChunkSize = 128 * 1024

	TunnelOpOpen  = "open"
	TunnelOpTX    = "tx"
	TunnelOpRX    = "rx"
	TunnelOpClose = "close"
	TunnelOpPing  = "ping"

	// Idle RX poll backoff (no local TX pending).
	tunnelMinReadWait = 30 * time.Millisecond
	tunnelMaxReadWait = 300 * time.Millisecond
	// RX bundled with TX — minimal wait so upload batches return quickly.
	tunnelRXWaitWithTX = 1 * time.Millisecond

	// MaxTXPerBatch caps tx ops per Apps Script POST (VPS executes sequentially).
	MaxTXPerBatch = 8

	// txCoalesceMaxWait holds small uploads back briefly so multiple chunks batch in one POST.
	txCoalesceMaxWait = 5 * time.Millisecond
)

// TunnelEnvelope is the Apps Script POST body for raw TCP tunnel ops.
// The client sends this only to fronted Apps Script URLs — not to the VPS.
// Use Batch to combine tx+rx in one Apps Script round trip; Req is a single op.
type TunnelEnvelope struct {
	Key   string          `json:"k"`
	Req   TunnelRequest   `json:"t,omitempty"`
	Batch []TunnelRequest `json:"tb,omitempty"`
}

// TunnelBatchResponse is returned by the VPS when multiple ops are submitted.
type TunnelBatchResponse struct {
	Results []TunnelResponse `json:"results"`
}

// TunnelRequest is one tunnel operation forwarded to the VPS /tunnel endpoint.
type TunnelRequest struct {
	Op     string `json:"op"`
	ID     string `json:"id"`
	Target string `json:"target,omitempty"`
	Data   string `json:"data,omitempty"`
	WaitMS int    `json:"wait_ms,omitempty"`
}

// TunnelResponse is returned by Apps Script and the VPS /tunnel handler.
type TunnelResponse struct {
	OK    bool   `json:"ok"`
	Data  string `json:"data,omitempty"`
	Error string `json:"e,omitempty"`
}

// ValidTunnelTarget reports whether target is a host:port suitable for TCP dial.
func ValidTunnelTarget(target string) bool {
	host, port, err := net.SplitHostPort(strings.TrimSpace(target))
	if err != nil || host == "" || port == "" {
		return false
	}
	return true
}

// NormalizeHostPort ensures host has an explicit TCP port.
func NormalizeHostPort(hostport, defaultPort string) string {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(hostport); err == nil {
		return hostport
	}
	if strings.Contains(hostport, ":") && strings.Count(hostport, ":") > 1 {
		return net.JoinHostPort(strings.Trim(hostport, "[]"), defaultPort)
	}
	return net.JoinHostPort(hostport, defaultPort)
}

// clampTunnelReadWait bounds idle RX poll interval on the client.
func clampTunnelReadWait(wait time.Duration) time.Duration {
	if wait < tunnelMinReadWait {
		return tunnelMinReadWait
	}
	if wait > tunnelMaxReadWait {
		return tunnelMaxReadWait
	}
	return wait
}

// tunnelRXWaitMS picks VPS read wait for a batched flush.
func tunnelRXWaitMS(hasPendingTX bool, idleWait time.Duration) int {
	if hasPendingTX {
		return int(tunnelRXWaitWithTX / time.Millisecond)
	}
	return int(clampTunnelReadWait(idleWait) / time.Millisecond)
}
