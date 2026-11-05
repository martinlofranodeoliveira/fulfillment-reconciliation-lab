package fixture

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/model"
)

// One extra byte is intentionally permitted for bounded oversized-body tests.
const MaxFixturePayloadBytes = model.MaxPayloadBytes + 1

// limitedListener rejects excess active connections without creating handlers.
type limitedListener struct {
	net.Listener
	active atomic.Int32
}
type limitedConn struct {
	net.Conn
	owner *limitedListener
	once  sync.Once
}

func (c *limitedConn) Close() error {
	var err error
	c.once.Do(func() { err = c.Conn.Close(); c.owner.active.Add(-1) })
	return err
}
func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.active.Add(1) > 8 {
			l.active.Add(-1)
			_ = conn.Close()
			continue
		}
		return &limitedConn{Conn: conn, owner: l}, nil
	}
}

type Server struct {
	listener   net.Listener
	server     *http.Server
	mu         sync.Mutex
	payload    []byte
	statusCode int
	closed     bool
	requests   atomic.Int64
	handlers   sync.WaitGroup
	done       chan struct{}
	closeOnce  sync.Once
	closeErr   error
}

func NewServer(initialPayload []byte) (*Server, error) {
	if len(initialPayload) > MaxFixturePayloadBytes {
		return nil, errors.New("fixture payload exceeds maximum")
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("fixture bind failed")
	}
	s := &Server{listener: &limitedListener{Listener: ln}, payload: append([]byte{}, initialPayload...), statusCode: 200, done: make(chan struct{})}
	s.server = &http.Server{
		Handler:           http.HandlerFunc(s.handleSnapshot),
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      2 * time.Second,
		IdleTimeout:       time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	go func() { defer close(s.done); _ = s.server.Serve(s.listener) }()
	return s, nil
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		w.WriteHeader(503)
		return
	}
	s.handlers.Add(1)
	code, body := s.statusCode, s.payload
	s.mu.Unlock()
	defer s.handlers.Done()
	if r.URL.Path != "/snapshot" || r.URL.RawQuery != "" {
		w.WriteHeader(404)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(405)
		return
	}
	s.requests.Add(1)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func (s *Server) URL() string         { return "http://" + s.listener.Addr().String() + "/snapshot" }
func (s *Server) RequestCount() int64 { return s.requests.Load() }

// SetResponse owns a copy; callers must not mutate a slice concurrently with
// the call itself. Existing in-flight responses keep their immutable old copy.
func (s *Server) SetResponse(statusCode int, payload []byte) error {
	if statusCode < 200 || statusCode > 599 {
		return errors.New("invalid fixture status")
	}
	if len(payload) > MaxFixturePayloadBytes {
		return errors.New("fixture payload exceeds maximum")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("fixture is closed")
	}
	s.statusCode, s.payload = statusCode, append([]byte{}, payload...)
	return nil
}

func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		if err := s.server.Close(); err != nil {
			s.closeErr = errors.New("fixture close failed")
		}
		<-s.done
		s.handlers.Wait()
	})
	return s.closeErr
}
