package tunnel

import "testing"

func TestIsRetryableAppsScriptError(t *testing.T) {
	tests := []struct {
		msg   string
		retry bool
	}{
		{"Exception: Service invoked too many times for one day: urlfetch.", true},
		{"quota exceeded", true},
		{"unauthorized", false},
		{"bad request", false},
		{"simulated failure", false},
	}
	for _, tc := range tests {
		if got := isRetryableAppsScriptError(tc.msg); got != tc.retry {
			t.Errorf("isRetryableAppsScriptError(%q) = %v, want %v", tc.msg, got, tc.retry)
		}
	}
}

func TestTunnelBodyShouldRetry_QuotaEnvelope(t *testing.T) {
	raw := []byte(`{"e":"Exception: Service invoked too many times for one day: urlfetch."}`)
	retry, err := tunnelBodyShouldRetry(raw)
	if !retry || err == nil {
		t.Fatalf("retry=%v err=%v", retry, err)
	}
}

func TestTunnelBodyShouldRetry_ErrorField(t *testing.T) {
	raw := []byte(`{"ok":false,"e":"quota"}`)
	retry, err := tunnelBodyShouldRetry(raw)
	if !retry || err == nil {
		t.Fatalf("retry=%v err=%v", retry, err)
	}
}

func TestTunnelBodyShouldRetry_OKBatch(t *testing.T) {
	raw := []byte(`{"results":[{"ok":true},{"ok":true,"data":""}]}`)
	retry, err := tunnelBodyShouldRetry(raw)
	if retry || err != nil {
		t.Fatalf("retry=%v err=%v", retry, err)
	}
}

func TestTunnelBodyShouldRetry_EmptyAndHTML(t *testing.T) {
	retry, err := tunnelBodyShouldRetry([]byte("  "))
	if !retry || err == nil {
		t.Fatalf("empty: retry=%v err=%v", retry, err)
	}
	retry, err = tunnelBodyShouldRetry([]byte("<html>quota</html>"))
	if !retry || err == nil {
		t.Fatalf("html: retry=%v err=%v", retry, err)
	}
}

func TestTunnelBodyShouldRetry_BatchItemQuota(t *testing.T) {
	raw := []byte(`{"results":[{"ok":false,"e":"quota exceeded"}]}`)
	retry, err := tunnelBodyShouldRetry(raw)
	if !retry || err == nil {
		t.Fatalf("retry=%v err=%v", retry, err)
	}
}

func TestTunnelBodyShouldRetry_BatchItemFatal(t *testing.T) {
	raw := []byte(`{"results":[{"ok":false,"e":"unauthorized"}]}`)
	retry, err := tunnelBodyShouldRetry(raw)
	if retry || err == nil {
		t.Fatalf("retry=%v err=%v", retry, err)
	}
}

func TestTunnelBodyShouldRetry_BareOKFalse(t *testing.T) {
	raw := []byte(`{"ok":false}`)
	retry, err := tunnelBodyShouldRetry(raw)
	if retry || err == nil {
		t.Fatalf("retry=%v err=%v", retry, err)
	}
}

func TestNormalizeHostPort(t *testing.T) {
	if got := NormalizeHostPort("example.com", "443"); got != "example.com:443" {
		t.Fatalf("host only = %q", got)
	}
	if got := NormalizeHostPort("example.com:8080", "443"); got != "example.com:8080" {
		t.Fatalf("with port = %q", got)
	}
	if got := NormalizeHostPort("", "443"); got != "" {
		t.Fatalf("empty = %q", got)
	}
}
