// Package httpclient builds HTTP clients for the app's outbound calls
// (Telegram API, LLM providers).
//
// The app runs in a container without an IPv6 route: when a provider
// resolves to an AAAA record, the dial fails with "network is
// unreachable". The transport therefore dials IPv4 only (tcp4), and the
// proxy still comes from the environment (HTTPS_PROXY etc.), so a proxy
// configured in .env is honored.
package httpclient

import (
	"context"
	"net"
	"net/http"
	"time"
)

const (
	dialTimeout = 30 * time.Second
	keepAlive   = 30 * time.Second
	tlsTimeout  = 10 * time.Second
	idleTimeout = 90 * time.Second
)

// New returns an HTTP client with the given overall timeout and an
// IPv4-only transport.
func New(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: keepAlive}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Force tcp4: an AAAA record has no route here, and
				// dialing it only burns time before failing.
				return dialer.DialContext(ctx, "tcp4", addr)
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       idleTimeout,
			TLSHandshakeTimeout:   tlsTimeout,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}
