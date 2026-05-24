package tunnel

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type tunnelTestStack struct {
	echoAddr string
	appsURL  string
	front    string
	client   *http.Client
	cleanup func()
}

func (s *tunnelTestStack) Close() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

func newTunnelTestStack(t *testing.T) *tunnelTestStack {
	t.Helper()

	echo := startIntegrationEcho(t)
	hub := newTestTunnelHub()

	vpsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleTestTunnel(w, r, hub)
	})

	apps := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var env TunnelEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			http.Error(w, "bad envelope", http.StatusBadRequest)
			return
		}
		var payload []byte
		if len(env.Batch) > 0 {
			payload, _ = json.Marshal(struct {
				Ops []TunnelRequest `json:"ops"`
			}{Ops: env.Batch})
		} else {
			payload, _ = json.Marshal(env.Req)
		}
		req, _ := http.NewRequest(http.MethodPost, "https://vps.local/tunnel", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		vpsHandler.ServeHTTP(rec, req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))

	return &tunnelTestStack{
		echoAddr: echo.Addr().String(),
		appsURL:  apps.URL,
		front:    testFrontDomain(apps),
		client:   apps.Client(),
		cleanup: func() {
			apps.Close()
			_ = echo.Close()
		},
	}
}

func TestTunnelIntegration_EchoRoundTrip(t *testing.T) {
	stack := newTunnelTestStack(t)
	defer stack.Close()

	tc := NewTunnelClient(stack.client, []string{stack.appsURL}, stack.front, "secret", 10*time.Second)
	sess, err := tc.OpenSession(context.Background(), stack.echoAddr)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close(context.Background())

	resps, err := sess.Exchange(context.Background(), []TunnelRequest{
		{Op: TunnelOpTX, Data: base64.StdEncoding.EncodeToString([]byte("ping"))},
		{Op: TunnelOpRX, WaitMS: 500},
	})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	data, err := base64.StdEncoding.DecodeString(resps[len(resps)-1].Data)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "pong" {
		t.Fatalf("got %q want pong", data)
	}
}

func TestTunnelIntegration_BridgeEcho(t *testing.T) {
	stack := newTunnelTestStack(t)
	defer stack.Close()

	tc := NewTunnelClient(stack.client, []string{stack.appsURL}, stack.front, "secret", 10*time.Second)
	sess, err := tc.OpenSession(context.Background(), stack.echoAddr)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		RunTunnelBridge(ctx, serverConn, sess, stack.echoAddr, tc.timeout)
		close(done)
	}()

	if _, err := io.WriteString(clientConn, "ping"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(clientConn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "pong" {
		t.Fatalf("got %q", buf)
	}
	_ = clientConn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not exit")
	}
}

func TestTunnelIntegration_CONNECTHandler(t *testing.T) {
	stack := newTunnelTestStack(t)
	defer stack.Close()

	pb, err := newTunnelBundle([]string{stack.appsURL}, stack.front, "secret", stack.client, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		handleRelayTunnelConnect(serverConn, stack.echoAddr, pb)
		close(done)
	}()

	br := bufio.NewReader(clientConn)
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" {
			break
		}
	}
	if _, err := io.WriteString(clientConn, "ping"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(clientConn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "pong" {
		t.Fatalf("body = %q", buf)
	}
	_ = clientConn.Close()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("handler timeout")
	}
}

type testTunnelHub struct {
	mu       sync.Mutex
	sessions map[string]net.Conn
}

func newTestTunnelHub() *testTunnelHub {
	return &testTunnelHub{sessions: make(map[string]net.Conn)}
}

func handleTestTunnel(w http.ResponseWriter, r *http.Request, hub *testTunnelHub) {
	body, _ := io.ReadAll(r.Body)
	var batch struct {
		Ops []TunnelRequest `json:"ops"`
	}
	if json.Unmarshal(body, &batch) == nil && len(batch.Ops) > 0 {
		results := make([]TunnelResponse, len(batch.Ops))
		for i, op := range batch.Ops {
			results[i] = hub.handle(op)
			if !results[i].OK {
				break
			}
		}
		_ = json.NewEncoder(w).Encode(TunnelBatchResponse{Results: results})
		return
	}
	var req TunnelRequest
	if json.Unmarshal(body, &req) != nil {
		_ = json.NewEncoder(w).Encode(TunnelResponse{Error: "bad json"})
		return
	}
	_ = json.NewEncoder(w).Encode(hub.handle(req))
}

func (h *testTunnelHub) handle(req TunnelRequest) TunnelResponse {
	switch req.Op {
	case TunnelOpOpen:
		conn, err := net.Dial("tcp", req.Target)
		if err != nil {
			return TunnelResponse{Error: err.Error()}
		}
		h.mu.Lock()
		h.sessions[req.ID] = conn
		h.mu.Unlock()
		return TunnelResponse{OK: true}
	case TunnelOpTX:
		h.mu.Lock()
		c := h.sessions[req.ID]
		h.mu.Unlock()
		if c == nil {
			return TunnelResponse{Error: "unknown session"}
		}
		data, err := base64.StdEncoding.DecodeString(req.Data)
		if err != nil {
			return TunnelResponse{Error: "bad base64"}
		}
		if _, err := c.Write(data); err != nil {
			return TunnelResponse{Error: err.Error()}
		}
		return TunnelResponse{OK: true}
	case TunnelOpRX:
		h.mu.Lock()
		c := h.sessions[req.ID]
		h.mu.Unlock()
		if c == nil {
			return TunnelResponse{Error: "unknown session"}
		}
		wait := time.Duration(req.WaitMS) * time.Millisecond
		if wait <= 0 {
			wait = time.Millisecond
		}
		if wait > 500*time.Millisecond {
			wait = 500 * time.Millisecond
		}
		_ = c.SetReadDeadline(time.Now().Add(wait))
		buf := make([]byte, 4096)
		n, err := c.Read(buf)
		_ = c.SetReadDeadline(time.Time{})
		if n > 0 {
			return TunnelResponse{OK: true, Data: base64.StdEncoding.EncodeToString(buf[:n])}
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return TunnelResponse{OK: true}
		}
		if err != nil {
			return TunnelResponse{Error: err.Error()}
		}
		return TunnelResponse{OK: true}
	case TunnelOpClose:
		h.mu.Lock()
		if c := h.sessions[req.ID]; c != nil {
			_ = c.Close()
			delete(h.sessions, req.ID)
		}
		h.mu.Unlock()
		return TunnelResponse{OK: true}
	default:
		return TunnelResponse{Error: "bad op"}
	}
}

func startIntegrationEcho(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 64)
				n, _ := conn.Read(buf)
				if n > 0 && string(buf[:n]) == "ping" {
					_, _ = conn.Write([]byte("pong"))
				}
			}(c)
		}
	}()
	return ln
}
