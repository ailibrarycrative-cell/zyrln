package mobile

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateCA_CreatesInstallableCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "certs", "ca.pem")
	keyPath := filepath.Join(dir, "certs", "ca.key")

	if err := GenerateCA(certPath, keyPath); err != "" {
		t.Fatalf("GenerateCA returned error: %s", err)
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}

	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatalf("generated cert/key pair is invalid: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("generated cert is not certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse generated cert: %v", err)
	}
	if !cert.IsCA || !cert.BasicConstraintsValid {
		t.Fatalf("generated cert is not a valid CA: IsCA=%v BasicConstraintsValid=%v", cert.IsCA, cert.BasicConstraintsValid)
	}
}

func TestStart_RequiresExistingCA(t *testing.T) {
	Stop()
	t.Cleanup(Stop)

	dir := t.TempDir()
	certPath := filepath.Join(dir, "certs", "ca.pem")
	keyPath := filepath.Join(dir, "certs", "ca.key")

	err := Start("", "secret", "127.0.0.1:0", certPath, keyPath)
	if !strings.Contains(err, "load CA:") {
		t.Fatalf("Start error = %q, want missing CA error", err)
	}
	if IsRunning() {
		t.Fatal("Start should not leave proxy running when CA is missing")
	}
	if _, statErr := os.Stat(certPath); !os.IsNotExist(statErr) {
		t.Fatalf("Start should not generate cert file implicitly, stat error: %v", statErr)
	}
	if _, statErr := os.Stat(keyPath); !os.IsNotExist(statErr) {
		t.Fatalf("Start should not generate key file implicitly, stat error: %v", statErr)
	}
}

func TestStart_LoadsExistingCA(t *testing.T) {
	Stop()
	t.Cleanup(Stop)

	dir := t.TempDir()
	certPath := filepath.Join(dir, "certs", "ca.pem")
	keyPath := filepath.Join(dir, "certs", "ca.key")
	if err := GenerateCA(certPath, keyPath); err != "" {
		t.Fatalf("GenerateCA returned error: %s", err)
	}

	err := Start("", "secret", "127.0.0.1:0", certPath, keyPath)
	if err != "" {
		t.Fatalf("Start with empty URL should succeed (direct-only mode), got error: %s", err)
	}
	if !IsRunning() {
		t.Fatal("Start should leave proxy running in direct-only mode")
	}
}

func TestLastError(t *testing.T) {
	Stop()
	t.Cleanup(Stop)

	errStr := Start("", "secret", "127.0.0.1:0", "/nonexistent/ca.pem", "/nonexistent/ca.key")
	if errStr == "" {
		t.Fatalf("expected error from Start")
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

func TestStart_WithRelayURL(t *testing.T) {
	Stop()
	t.Cleanup(Stop)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"s": 200,
			"h": map[string]any{"content-type": []string{"text/plain"}},
			"b": base64.StdEncoding.EncodeToString([]byte("relay-ok")),
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.pem")
	keyPath := filepath.Join(dir, "ca.key")
	if err := GenerateCA(certPath, keyPath); err != "" {
		t.Fatalf("GenerateCA: %s", err)
	}

	addr := freeListenAddr(t)
	if err := Start(srv.URL, "secret", addr, certPath, keyPath); err != "" {
		t.Fatalf("Start: %s", err)
	}
	if !IsRunning() {
		t.Fatal("expected proxy running")
	}
	if logs := PollLogs(); !strings.Contains(logs, "Proxy started") {
		t.Fatalf("PollLogs = %q, want startup message", logs)
	}
}
