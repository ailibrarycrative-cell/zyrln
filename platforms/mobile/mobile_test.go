package mobile

import (
	"net"
	"strings"
	"testing"
)

func TestLastError(t *testing.T) {
	Stop()
	t.Cleanup(Stop)

	errStr := StartTunnel("", "", "127.0.0.1:0")
	if errStr == "" {
		t.Fatalf("expected error from StartTunnel")
	}
	if got := LastError(); got != errStr {
		t.Fatalf("LastError = %q, want %q", got, errStr)
	}
}

func TestStop_WhenNotRunning(t *testing.T) {
	Stop()
	Stop() // Should not panic
	if IsRunning() {
		t.Errorf("expected IsRunning() to be false")
	}
}

func freeListenAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func TestStartDirect_StartsAndLogs(t *testing.T) {
	Stop()
	t.Cleanup(Stop)
	PollLogs() // drain

	addr := freeListenAddr(t)
	if err := StartDirect(addr); err != "" {
		t.Fatalf("StartDirect: %s", err)
	}
	if !IsRunning() {
		t.Fatal("expected proxy running after StartDirect")
	}
	logs := PollLogs()
	if !strings.Contains(logs, "Direct proxy started") {
		t.Fatalf("PollLogs = %q, want startup message", logs)
	}
	if PollLogs() != "" {
		t.Fatal("second PollLogs should be empty until new entries")
	}
}

func TestPollLogs_AfterStop(t *testing.T) {
	Stop()
	t.Cleanup(Stop)
	PollLogs()

	addr := freeListenAddr(t)
	if err := StartDirect(addr); err != "" {
		t.Fatalf("StartDirect: %s", err)
	}
	PollLogs()

	Stop()
	if logs := PollLogs(); !strings.Contains(logs, "Proxy stopped") {
		t.Fatalf("PollLogs after Stop = %q, want stop message", logs)
	}
}

func TestGetAllLogs_IncludesBuffer(t *testing.T) {
	Stop()
	t.Cleanup(Stop)

	addr := freeListenAddr(t)
	if err := StartDirect(addr); err != "" {
		t.Fatalf("StartDirect: %s", err)
	}
	all := GetAllLogs()
	if !strings.Contains(all, "Direct proxy started") {
		t.Fatalf("GetAllLogs = %q, want startup entry", all)
	}
}

func TestPing_NoRelayURL(t *testing.T) {
	Stop()
	got := Ping("", "")
	if !strings.HasPrefix(got, "error:") {
		t.Fatalf("Ping = %q, want error prefix", got)
	}
	if !strings.Contains(got, "no relay URL") {
		t.Fatalf("Ping = %q, want no relay URL error", got)
	}
}

func TestSetDirectEnabled_Wrapper(t *testing.T) {
	orig := IsDirectEnabled()
	t.Cleanup(func() { SetDirectEnabled(orig) })

	SetDirectEnabled(false)
	if IsDirectEnabled() {
		t.Fatal("expected direct disabled")
	}
	SetDirectEnabled(true)
	if !IsDirectEnabled() {
		t.Fatal("expected direct enabled")
	}
}
