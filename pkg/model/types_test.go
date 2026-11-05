package model_test

import (
	"strings"
	"testing"

	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/model"
)

func metadata(source string, count int) model.SnapshotMetadata {
	return model.SnapshotMetadata{SchemaVersion: "0.1.0", Source: source, SnapshotID: "snapshot", CutoffISO: "2026-11-05T12:00:00Z", RecordCount: count}
}

func TestBusinessIDBoundaries(t *testing.T) {
	for _, id := range []string{"ord-1001", "a:b.c_d", strings.Repeat("a", 128)} {
		if err := model.ValidateBusinessID("id", id); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"", strings.Repeat("a", 129), "bad id", "a\nb", "é", "/tmp"} {
		if err := model.ValidateBusinessID("secret-field", id); err == nil || strings.Contains(err.Error(), "secret-field") {
			t.Fatal("identifier validation or diagnostic sanitization failed")
		}
	}
}

func TestOrderTupleAndDuplicatePolicy(t *testing.T) {
	s := model.OrderSnapshot{Metadata: metadata("orders", 2), Orders: []model.OrderRecord{
		{OrderID: "a:b", SKUID: "c", RequestedUnits: 5},
		{OrderID: "a", SKUID: "b:c", RequestedUnits: 4},
	}}
	if err := s.Validate(); err != nil {
		t.Fatalf("distinct tuples collided: %v", err)
	}
	s.Orders[1] = s.Orders[0]
	if err := s.Validate(); err == nil {
		t.Fatal("duplicate order tuple accepted")
	}
}

func TestMetadataAndTimeValidation(t *testing.T) {
	for _, cutoff := range []string{"garbage", "2026-11-05T12:00:00+00:00", "2026-11-05T12:00:00.0Z", "2026-02-29T12:00:00Z", "0000-01-01T00:00:00Z"} {
		s := model.OrderSnapshot{Metadata: metadata("orders", 0)}
		s.Metadata.CutoffISO = cutoff
		if s.Validate() == nil {
			t.Fatal("noncanonical or invalid cutoff accepted")
		}
	}
	s := model.OrderSnapshot{Metadata: metadata("orders", 1)}
	if s.Validate() == nil {
		t.Fatal("record count mismatch accepted")
	}
	s.Metadata = metadata("orders", 0)
	s.Metadata.SchemaVersion = "9"
	if s.Validate() == nil {
		t.Fatal("unsupported version accepted")
	}
	var absent *model.OrderSnapshot
	if absent.Validate() == nil {
		t.Fatal("nil receiver accepted")
	}
}

func TestEventTimeAndShipmentUniqueness(t *testing.T) {
	e := model.ReservationEvent{EventID: "e1", OrderID: "o1", SKUID: "s1", ReservedUnits: 5, OccurredAtISO: "2026-11-05T12:00:00Z"}
	r := model.ReservationSnapshot{Metadata: metadata("reservations", 2), Events: []model.ReservationEvent{e, e}}
	if err := r.Validate(); err != nil {
		t.Fatalf("identical reservation replay must be permitted: %v", err)
	}
	r.Events[1].OccurredAtISO = "2026-11-05T12:00:01Z"
	if r.Validate() == nil {
		t.Fatal("event after cutoff accepted")
	}
	x := model.ShipmentRecord{ShipmentID: "x1", OrderID: "o1", SKUID: "s1", ShippedUnits: 5, ShippedAtISO: "2026-11-05T12:00:00Z"}
	s := model.ShipmentSnapshot{Metadata: metadata("shipments", 2), Shipments: []model.ShipmentRecord{x, x}}
	if s.Validate() == nil {
		t.Fatal("shipment IDs must be unique, even for identical rows")
	}
	s.Shipments[1].ShipmentID = "x2"
	s.Shipments[1].ShippedAtISO = "2026-11-05T12:00:01Z"
	if s.Validate() == nil {
		t.Fatal("shipment after cutoff accepted")
	}
}
