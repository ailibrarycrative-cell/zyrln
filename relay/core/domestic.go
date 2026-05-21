package core

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// domesticBypassEnabled routes Iranian domains outside the Apps Script relay.
var domesticBypassEnabled atomic.Bool

func init() {
	domesticBypassEnabled.Store(true)
}

func SetDomesticBypassEnabled(v bool) { domesticBypassEnabled.Store(v) }
func GetDomesticBypassEnabled() bool  { return domesticBypassEnabled.Load() }

var (
	domesticRules   atomic.Pointer[domesticMatcher]
	domesticRefresh sync.Mutex
	domesticOnce    sync.Once
)

type domesticMatcher struct {
	// roots: domain entries from the bundled list (e.g. digikala.com).
	// A host matches if it equals a root or is a subdomain (www.digikala.com).
	roots map[string]struct{}
}

func (m *domesticMatcher) addRoot(domain string) {
	if m.roots == nil {
		m.roots = make(map[string]struct{})
	}
	h := normalizeHost(domain)
	if h != "" {
		m.roots[h] = struct{}{}
	}
}

func (m *domesticMatcher) matchHost(host string) bool {
	h := normalizeHost(host)
	if h == "" {
		return false
	}
	if matchIRTLD(h) {
		return true
	}
	if m == nil || len(m.roots) == 0 {
		return false
	}
	for _, name := range parentDomains(h) {
		if _, ok := m.roots[name]; ok {
			return true
		}
	}
	return false
}

// parentDomains returns the host and registrable parent names (not bare TLDs).
// e.g. www.digikala.com → [www.digikala.com, digikala.com]
// .ir hosts are handled separately by matchIRTLD.
func parentDomains(host string) []string {
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return []string{host}
	}
	out := make([]string, 0, len(labels)-1)
	for i := 0; i < len(labels)-1; i++ {
		out = append(out, strings.Join(labels[i:], "."))
	}
	return out
}

func matchIRTLD(host string) bool {
	return host == "ir" || strings.HasSuffix(host, ".ir") ||
		strings.HasSuffix(host, ".xn--mgba3a4f16a") // .ir punycode
}

func normalizeHost(host string) string {
	h := strings.TrimSpace(strings.ToLower(host))
	if h == "" {
		return ""
	}
	if strings.Contains(h, ":") {
		if name, _, err := net.SplitHostPort(h); err == nil {
			h = name
		}
	}
	return strings.TrimSuffix(h, ".")
}

// RefreshDomesticRules reloads the bundled domain list from the embedded file (no network).
// Safe to call more than once (e.g. after tests or a future in-app update hook).
func RefreshDomesticRules() {
	go reloadDomesticRules()
}

func ensureDomesticRefresh() {
	domesticOnce.Do(reloadDomesticRules)
}

func reloadDomesticRules() {
	domesticRefresh.Lock()
	defer domesticRefresh.Unlock()
	if err := loadBundledDomesticRules(); err != nil {
		logf("error", "domestic rules: %v", err)
		if domesticRules.Load() == nil {
			domesticRules.Store(&domesticMatcher{})
		}
		return
	}
	n := 0
	if m := domesticRules.Load(); m != nil {
		n = len(m.roots)
	}
	logf("system", "domestic bypass: %d domains (bundled)", n)
}

// domesticBypassForMode is true when traffic would otherwise use the relay (relay-only
// or direct+relay). Pure direct-only mode is excluded — that path is already all local.
func domesticBypassForMode(mode proxyMode) bool {
	switch mode {
	case modeRelay, modeDirectRelay:
		return GetDomesticBypassEnabled()
	default:
		return false
	}
}

// ShouldUseDomesticBypass reports whether host should skip relay using only bundled
// plain-text rules (.ir and domain list suffix match). No DNS lookups.
func ShouldUseDomesticBypass(host string) bool {
	if !GetDomesticBypassEnabled() {
		return false
	}
	ensureDomesticRefresh()
	h := normalizeHost(host)
	if h == "" {
		return false
	}
	if IsGoogleDomain(h) {
		return false
	}
	if m := domesticRules.Load(); m != nil {
		return m.matchHost(h)
	}
	return matchIRTLD(h)
}

// handlePlainConnect dials the target directly (protected on Android) without relay or TLS fragmentation.
func handlePlainConnect(clientConn net.Conn, targetHost string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	serverConn, err := protectedDialer(15 * time.Second).DialContext(ctx, "tcp", targetHost)
	if err != nil {
		logf("error", "domestic %s: %v", targetHost, err)
		_, _ = clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer serverConn.Close()
	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	logf("info", "domestic %s", targetHost)
	pipe(clientConn, serverConn)
}

// dialPlainDirect is used by SOCKS when the target should bypass relay.
func dialPlainDirect(targetHost string) (net.Conn, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := protectedDialer(15 * time.Second).DialContext(ctx, "tcp", targetHost)
	if err != nil {
		logf("error", "domestic %s: %v", targetHost, err)
		return nil, false
	}
	logf("info", "domestic %s", targetHost)
	return conn, true
}
