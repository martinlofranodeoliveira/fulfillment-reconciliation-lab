package fixture_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/client"
	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/fixture"
)

func TestFixtureOwnershipLifecycleAndActualRequests(t *testing.T) {
	payload := []byte(`{"synthetic":true}`)
	s, err := fixture.NewServer(payload)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	payload[0] = 'x'
	c := client.NewLoopbackClient(time.Second)
	defer c.Close()
	body, err := c.FetchRaw(context.Background(), s.URL())
	if err != nil || string(body) != `{"synthetic":true}` {
		t.Fatal("constructor retained caller bytes")
	}
	updated := []byte(`{"second":true}`)
	if err := s.SetResponse(200, updated); err != nil {
		t.Fatal(err)
	}
	updated[0] = 'x'
	body, err = c.FetchRaw(context.Background(), s.URL())
	if err != nil || string(body) != `{"second":true}` {
		t.Fatal("SetResponse retained caller bytes")
	}
	if s.RequestCount() != 2 {
		t.Fatal("actual requests were not counted")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal("close is not idempotent")
	}
	if _, err := c.FetchRaw(context.Background(), s.URL()); err == nil {
		t.Fatal("closed fixture still responds")
	}
	if err := s.SetResponse(200, nil); err == nil {
		t.Fatal("closed fixture accepted mutation")
	}
}

func TestFixtureBounds(t *testing.T) {
	if s, err := fixture.NewServer([]byte(strings.Repeat("x", fixture.MaxFixturePayloadBytes+1))); err == nil {
		s.Close()
		t.Fatal("unbounded fixture accepted")
	}
	s, err := fixture.NewServer(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, status := range []int{0, 101, 600, 999} {
		if s.SetResponse(status, nil) == nil {
			t.Fatal("invalid status accepted")
		}
	}
	if s.SetResponse(200, []byte(strings.Repeat("x", fixture.MaxFixturePayloadBytes+1))) == nil {
		t.Fatal("oversized replacement accepted")
	}
}

func TestConcurrentReplacementUsesImmutableCopies(t *testing.T) {
	s, err := fixture.NewServer([]byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := client.NewLoopbackClient(time.Second)
	defer c.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			p := []byte("other")
			if err := s.SetResponse(200, p); err != nil {
				t.Error(err)
				return
			}
			p[0] = 'X'
		}
	}()
	for i := 0; i < 30; i++ {
		body, err := c.FetchRaw(context.Background(), s.URL())
		if err != nil || (string(body) != "first" && string(body) != "other") {
			t.Errorf("torn fixture response: %v", err)
		}
	}
	wg.Wait()
	if s.RequestCount() != 30 {
		t.Fatal("request counter mismatch")
	}
}
