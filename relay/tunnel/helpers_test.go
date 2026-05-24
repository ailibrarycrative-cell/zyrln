package tunnel

import (
	"net"
	"net/http/httptest"
	"strings"
)

// testFrontDomain is the domain-front host for httptest servers (must include port).
func testFrontDomain(srv *httptest.Server) string {
	return srv.Listener.Addr().String()
}

func srvHost(srv *httptest.Server) string {
	host := srv.URL
	if strings.HasPrefix(host, "https://") {
		host = host[len("https://"):]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
