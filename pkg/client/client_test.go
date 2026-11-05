package client

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/fixture"
	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/model"
)

func localHTTP(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewUnstartedServer(handler)
	if err := s.Listener.Close(); err != nil {
		t.Fatal(err)
	}
	s.Listener = listener
	s.Config.ReadHeaderTimeout = time.Second
	s.Config.WriteTimeout = 2 * time.Second
	s.Start()
	t.Cleanup(s.Close)
	return s
}

func TestInvalidURLsNeverReachDialerAndDoNotLeak(t *testing.T) {
	c := NewLoopbackClient(time.Second)
	defer c.Close()
	calls := 0
	c.transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("must not dial")
	}
	for _, endpoint := range []string{
		"http://example.invalid:80/snapshot", "http://localhost:80/snapshot", "http://127.1:80/snapshot",
		"http://127.0.0.2:80/snapshot", "http://[::1]:80/snapshot", "https://127.0.0.1:80/snapshot",
		"http://private-user:private-password@127.0.0.1:80/snapshot", "http://127.0.0.1:80/private-path",
		"http://127.0.0.1:80/snapshot?private-query", "http://127.0.0.1:80/snapshot#private-fragment",
		"http://127.0.0.1:80/snapshot?", "http://127.0.0.1/snapshot", "http://127.0.0.1:080/snapshot",
		"http://127.0.0.1:80/snapshot#",
		"http://127.0.0.1:0/snapshot", "http://127.0.0.1:65536/snapshot", "http://%private/snapshot",
	} {
		_, err := c.FetchRaw(context.Background(), endpoint)
		if err == nil || err.Error() != "invalid loopback fixture URL" {
			t.Fatalf("URL not safely rejected: %v", err)
		}
	}
	if calls != 0 {
		t.Fatal("rejected URL reached a dialer")
	}
}

func TestDialGuardIndependentlyRejectsDNSAndOtherInterfaces(t *testing.T) {
	c := NewLoopbackClient(time.Second)
	defer c.Close()
	if c.transport.Proxy != nil {
		t.Fatal("proxy must be disabled")
	}
	for _, address := range []string{"localhost:80", "example.invalid:80", "192.0.2.1:80", "0.0.0.0:80", "[::1]:80"} {
		conn, err := c.transport.DialContext(context.Background(), "tcp", address)
		if conn != nil || err == nil || err.Error() != "fixture dial prohibited" {
			t.Fatal("dial guard did not reject before network resolution")
		}
	}
}

func TestProxyIgnoredAndRedirectDestinationNotContacted(t *testing.T) {
	var destinationRequests atomic.Int64
	destination := localHTTP(t, func(w http.ResponseWriter, r *http.Request) { destinationRequests.Add(1); w.WriteHeader(200) })
	t.Setenv("HTTP_PROXY", destination.URL)
	t.Setenv("HTTPS_PROXY", destination.URL)
	t.Setenv("ALL_PROXY", destination.URL)
	t.Setenv("NO_PROXY", "")
	var sourceRequests atomic.Int64
	source := localHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		sourceRequests.Add(1)
		http.Redirect(w, r, destination.URL+"/snapshot", http.StatusFound)
	})
	c := NewLoopbackClient(time.Second)
	defer c.Close()
	if _, err := c.FetchRaw(context.Background(), source.URL+"/snapshot"); err == nil || err.Error() != "http redirects are strictly disabled" {
		t.Fatalf("redirect was not rejected: %v", err)
	}
	if sourceRequests.Load() != 1 || destinationRequests.Load() != 0 {
		t.Fatal("proxy or redirect destination was contacted")
	}
}

func TestActualFixtures500AndOversizedBodies(t *testing.T) {
	s, err := fixture.NewServer([]byte(`{"synthetic":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := NewLoopbackClient(time.Second)
	defer c.Close()
	body, err := c.FetchRaw(context.Background(), s.URL())
	if err != nil || string(body) != `{"synthetic":true}` {
		t.Fatalf("actual HTTP fetch failed: %v", err)
	}
	if err := s.SetResponse(500, []byte("private-response-payload")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchRaw(context.Background(), s.URL()); err == nil || err.Error() != "server returned unexpected status code 500" {
		t.Fatalf("500 was not sanitized: %v", err)
	}
	if err := s.SetResponse(200, []byte(strings.Repeat("x", model.MaxPayloadBytes+1))); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchRaw(context.Background(), s.URL()); err == nil || err.Error() != "payload length exceeds maximum limit" {
		t.Fatalf("oversized fixture accepted: %v", err)
	}
	if s.RequestCount() != 3 {
		t.Fatal("fixtures did not receive the three real requests")
	}
}

func TestChunkedOversizeHeaderBoundAndTimeout(t *testing.T) {
	c := NewLoopbackClient(100 * time.Millisecond)
	defer c.Close()
	chunked := localHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte(strings.Repeat("x", model.MaxPayloadBytes+1)))
	})
	// Use a longer bounded timeout for the byte-bound test, independent of timing.
	bodyClient := NewLoopbackClient(2 * time.Second)
	defer bodyClient.Close()
	if _, err := bodyClient.FetchRaw(context.Background(), chunked.URL+"/snapshot"); err == nil || err.Error() != "payload length exceeds maximum limit" {
		t.Fatalf("streamed body cap failed: %v", err)
	}
	headers := localHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Synthetic", strings.Repeat("private", 10000))
		w.WriteHeader(200)
	})
	if _, err := bodyClient.FetchRaw(context.Background(), headers.URL+"/snapshot"); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("oversized headers accepted or leaked")
	}
	slow := localHTTP(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	start := time.Now()
	if _, err := c.FetchRaw(context.Background(), slow.URL+"/snapshot"); err == nil || err.Error() != "fixture request failed" {
		t.Fatalf("timeout not sanitized: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout was not bounded")
	}
}

func TestClientTimeoutClampAndNativeErrorSanitization(t *testing.T) {
	c := NewLoopbackClient(time.Hour)
	defer c.Close()
	if c.httpClient.Timeout != 5*time.Second || c.transport.MaxResponseHeaderBytes != 16*1024 {
		t.Fatal("client resource bounds changed")
	}
	c.transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("private-network-error")
	}
	if _, err := c.FetchRaw(context.Background(), "http://127.0.0.1:1/snapshot"); err == nil || err.Error() != "fixture request failed" {
		t.Fatal("native network error leaked")
	}
}
