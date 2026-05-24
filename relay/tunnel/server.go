package tunnel

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"zyrln/relay/core"
)

// tunnelBundle holds the raw TCP tunnel client for Android VPN mode.
// When direct mode is enabled, Google domains bypass the tunnel via TLS fragmentation.
type tunnelBundle struct {
	tunnel *TunnelClient
}

var activeTunnel atomic.Pointer[TunnelClient]

// ActiveTunnelClient returns the tunnel client used by the running proxy, if any.
func ActiveTunnelClient() *TunnelClient {
	return activeTunnel.Load()
}

// StopActiveTunnel stops keepalive on the proxy's tunnel client.
func StopActiveTunnel() {
	if t := activeTunnel.Swap(nil); t != nil {
		t.Stop()
	}
	StopPrewarm()
}

func newTunnelBundle(appScriptURLs []string, frontDomain, authKey string, client *http.Client, timeout time.Duration) (*tunnelBundle, error) {
	pb := &tunnelBundle{}
	if len(appScriptURLs) > 0 {
		pb.tunnel = adoptOrCreateTunnelClient(client, appScriptURLs, frontDomain, authKey, timeout)
		activeTunnel.Store(pb.tunnel)
	}
	return pb, nil
}

// StartTunnelProxy builds the local HTTP CONNECT proxy backed by the raw TCP tunnel.
func StartTunnelProxy(listenAddr string, appScriptURLs []string, frontDomain, authKey string, client *http.Client, timeout time.Duration) (*http.Server, error) {
	pb, err := newTunnelBundle(appScriptURLs, frontDomain, authKey, client, timeout)
	if err != nil {
		return nil, err
	}
	return buildTunnelHTTPProxyServer(listenAddr, pb), nil
}

func buildTunnelHTTPProxyServer(listenAddr string, pb *tunnelBundle) *http.Server {
	return &http.Server{
		Addr: listenAddr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				handleTunnelConnect(w, r, pb)
			} else {
				handleTunnelHTTP(w, r, pb)
			}
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func handleTunnelHTTP(w http.ResponseWriter, r *http.Request, pb *tunnelBundle) {
	if pb.tunnel == nil {
		http.Error(w, "no tunnel configured", http.StatusBadGateway)
		return
	}
	http.Error(w, "plain HTTP requires HTTPS CONNECT tunnel", http.StatusBadGateway)
}

func handleTunnelConnect(w http.ResponseWriter, r *http.Request, pb *tunnelBundle) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	rawConn, rw, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer rawConn.Close()
	local := &core.BufferedConn{Conn: rawConn, Reader: rw.Reader}

	if pb.tunnel == nil {
		_, _ = rawConn.Write([]byte("HTTP/1.1 502 No tunnel configured\r\n\r\n"))
		return
	}
	handleRelayTunnelConnect(local, r.Host, pb)
}

func connectHost(targetHost string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(targetHost))
	if err != nil {
		return strings.TrimSpace(targetHost)
	}
	return host
}

func handleRelayTunnelConnect(local io.ReadWriter, targetHost string, pb *tunnelBundle) {
	target := NormalizeHostPort(targetHost, "443")
	if core.IsDirectDomain(connectHost(targetHost)) {
		if c, ok := local.(net.Conn); ok {
			core.Log("info", "direct CONNECT %s", target)
			core.HandleDirectConnect(c, target)
			return
		}
	}
	if pb.tunnel == nil {
		if c, ok := local.(net.Conn); ok {
			_, _ = c.Write([]byte("HTTP/1.1 502 No tunnel configured\r\n\r\n"))
		}
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pb.tunnel.waitWarmupBeforeFirstConnect(ctx)

	sess, err := pb.tunnel.NewSession(target)
	if err != nil {
		if c, ok := local.(net.Conn); ok {
			_, _ = c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n"))
		}
		core.Log("error", "tunnel CONNECT session %s: %v", target, err)
		return
	}
	if c, ok := local.(net.Conn); ok {
		_, _ = c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	}
	core.Log("info", "tunnel CONNECT %s", target)
	RunTunnelBridge(ctx, local, sess, target, pb.tunnel.timeout)
}
