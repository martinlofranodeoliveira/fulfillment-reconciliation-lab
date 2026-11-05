package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/model"
	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/strictjson"
)

type SnapshotClient struct {
	httpClient *http.Client
	transport  *http.Transport
}

// NewLoopbackClient clamps the total timeout to at most five seconds.
func NewLoopbackClient(timeout time.Duration) *SnapshotClient {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || host != "127.0.0.1" || !validPort(port) {
				return nil, errors.New("fixture dial prohibited")
			}
			return dialer.DialContext(ctx, "tcp4", net.JoinHostPort(host, port))
		},
		DisableCompression:     true,
		MaxConnsPerHost:        2,
		MaxIdleConns:           2,
		MaxIdleConnsPerHost:    2,
		IdleConnTimeout:        time.Second,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: 16 * 1024,
	}
	return &SnapshotClient{transport: transport, httpClient: &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func validPort(port string) bool {
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535 && strconv.Itoa(n) == port
}

func isLoopbackHost(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "http" || u.Opaque != "" || u.User != nil ||
		u.Hostname() != "127.0.0.1" || !validPort(u.Port()) ||
		u.Host != net.JoinHostPort("127.0.0.1", u.Port()) || u.Path != "/snapshot" ||
		u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || strings.Contains(rawURL, "#") {
		return errors.New("invalid loopback fixture URL")
	}
	return nil
}

// Close releases idle transport connections. Call after all fetches complete.
func (c *SnapshotClient) Close() {
	if c != nil && c.transport != nil {
		c.transport.CloseIdleConnections()
	}
}

func (c *SnapshotClient) FetchRaw(ctx context.Context, endpointURL string) ([]byte, error) {
	if err := isLoopbackHost(endpointURL); err != nil {
		return nil, err
	}
	if ctx == nil || c == nil || c.httpClient == nil {
		return nil, errors.New("invalid fixture client context")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL, nil)
	if err != nil {
		return nil, errors.New("fixture request creation failed")
	}
	req.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.New("fixture request failed")
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode <= 399 {
		return nil, errors.New("http redirects are strictly disabled")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned unexpected status code %d", response.StatusCode)
	}
	if response.ContentLength > model.MaxPayloadBytes {
		return nil, errors.New("payload length exceeds maximum limit")
	}
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return nil, errors.New("encoded fixture responses are prohibited")
	}
	return strictjson.ReadBounded(response.Body)
}

func (c *SnapshotClient) FetchOrderSnapshot(ctx context.Context, endpointURL string) (*model.OrderSnapshot, error) {
	data, err := c.FetchRaw(ctx, endpointURL)
	if err != nil {
		return nil, err
	}
	return strictjson.DecodeOrderSnapshot(bytes.NewReader(data))
}
func (c *SnapshotClient) FetchReservationSnapshot(ctx context.Context, endpointURL string) (*model.ReservationSnapshot, error) {
	data, err := c.FetchRaw(ctx, endpointURL)
	if err != nil {
		return nil, err
	}
	return strictjson.DecodeReservationSnapshot(bytes.NewReader(data))
}
func (c *SnapshotClient) FetchShipmentSnapshot(ctx context.Context, endpointURL string) (*model.ShipmentSnapshot, error) {
	data, err := c.FetchRaw(ctx, endpointURL)
	if err != nil {
		return nil, err
	}
	return strictjson.DecodeShipmentSnapshot(bytes.NewReader(data))
}
