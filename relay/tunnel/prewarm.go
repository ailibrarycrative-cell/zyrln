package tunnel

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	tunnelWarmupTimeout        = 25 * time.Second
	tunnelFirstConnectWarmWait = 3 * time.Second
	// tunnelUserIdleGrace suppresses keepalive/deep-warm while the user is browsing.
	tunnelUserIdleGrace = 45 * time.Second
)

// Prewarm starts warming Apps Script + VPS in the background before Connect.
// Safe to call when the user selects a profile or imports config.
func Prewarm(client *http.Client, appScriptURLs []string, frontDomain, authKey string, timeout time.Duration) {
	if len(appScriptURLs) == 0 || strings.TrimSpace(authKey) == "" {
		return
	}
	key := prewarmKey(appScriptURLs, authKey)
	prewarmMu.Lock()
	defer prewarmMu.Unlock()
	if prewarmClient != nil && prewarmSig == key {
		if !prewarmClient.WarmReady() {
			prewarmClient.startWarmup()
		}
		return
	}
	if prewarmClient != nil {
		prewarmClient.Stop()
		prewarmClient = nil
	}
	prewarmClient = newPrewarmClient(client, appScriptURLs, frontDomain, authKey, timeout)
	prewarmSig = key
}

func newPrewarmClient(client *http.Client, appScriptURLs []string, frontDomain, authKey string, timeout time.Duration) *TunnelClient {
	c := &TunnelClient{
		appScriptURLs:  append([]string(nil), appScriptURLs...),
		frontDomain:    frontDomain,
		authKey:        authKey,
		timeout:        timeout,
		stopCh:         make(chan struct{}),
		deferKeepalive: true,
	}
	c.UseHTTPClient(client)
	if len(c.appScriptURLs) > 0 {
		c.startWarmup()
	}
	return c
}

// StopPrewarm stops a background prewarm client that was not adopted by the proxy.
func StopPrewarm() {
	prewarmMu.Lock()
	defer prewarmMu.Unlock()
	if prewarmClient != nil {
		prewarmClient.Stop()
		prewarmClient = nil
		prewarmSig = ""
	}
}

func adoptOrCreateTunnelClient(client *http.Client, appScriptURLs []string, frontDomain, authKey string, timeout time.Duration) *TunnelClient {
	key := prewarmKey(appScriptURLs, authKey)
	prewarmMu.Lock()
	if prewarmClient != nil && prewarmSig == key {
		tc := prewarmClient
		prewarmClient = nil
		prewarmSig = ""
		prewarmMu.Unlock()
		tc.abortWarmupForUser()
		waitCtx, waitCancel := context.WithTimeout(context.Background(), tunnelWarmupTimeout)
		_ = tc.WaitWarmup(waitCtx)
		waitCancel()
		tc.UseHTTPClient(client)
		tc.Activate()
		return tc
	}
	prewarmMu.Unlock()
	return NewTunnelClient(client, appScriptURLs, frontDomain, authKey, timeout)
}

var (
	prewarmMu      sync.Mutex
	prewarmClient  *TunnelClient
	prewarmSig     string
)

func prewarmKey(urls []string, authKey string) string {
	return strings.Join(urls, "|") + "\x00" + authKey
}
