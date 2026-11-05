package reconcile_test

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/model"
	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/reconcile"
)

const cutoff = "2026-11-05T12:00:00Z"

func meta(source string, count int) model.SnapshotMetadata {
	return model.SnapshotMetadata{SchemaVersion: "0.1.0", Source: source, SnapshotID: "snapshot", CutoffISO: cutoff, RecordCount: count}
}
func base() (*model.OrderSnapshot, *model.ReservationSnapshot, *model.ShipmentSnapshot) {
	return &model.OrderSnapshot{Metadata: meta("orders", 1), Orders: []model.OrderRecord{{OrderID: "o1", SKUID: "s1", RequestedUnits: 5}}},
		&model.ReservationSnapshot{Metadata: meta("reservations", 1), Events: []model.ReservationEvent{{EventID: "e1", OrderID: "o1", SKUID: "s1", ReservedUnits: 5, OccurredAtISO: cutoff}}},
		&model.ShipmentSnapshot{Metadata: meta("shipments", 2), Shipments: []model.ShipmentRecord{
			{ShipmentID: "x1", OrderID: "o1", SKUID: "s1", ShippedUnits: 3, ShippedAtISO: cutoff},
			{ShipmentID: "x2", OrderID: "o1", SKUID: "s1", ShippedUnits: 2, ShippedAtISO: cutoff},
		}}
}
func encode(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func run(t *testing.T, o *model.OrderSnapshot, r *model.ReservationSnapshot, s *model.ShipmentSnapshot) *model.ReconciliationReport {
	t.Helper()
	report, err := reconcile.Reconcile(o, r, s)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestHandDerivedQuantityOracles(t *testing.T) {
	o, r, s := base()
	r.Events = append(r.Events, r.Events[0])
	r.Metadata.RecordCount = 2
	report := run(t, o, r, s)
	f := report.Findings[0]
	if f.Classification != model.ClassificationMatched || f.RequestedUnits != 5 || f.ReservedUnits != 5 || f.ShippedUnits != 5 || f.DifferenceRequestedVsShipped != 0 || len(f.ReservationEventIDs) != 1 {
		t.Fatalf("5 = 5 = 3+2, with replay, must match: %+v", f)
	}
	s.Shipments = s.Shipments[:1]
	s.Metadata.RecordCount = 1
	f = run(t, o, r, s).Findings[0]
	if f.Classification != model.ClassificationInconsistentQuantities || f.ShippedUnits != 3 || f.DifferenceRequestedVsShipped != 2 || f.DifferenceReservedVsShipped != 2 {
		t.Fatalf("partial shipment oracle failed: %+v", f)
	}
	if !reflect.DeepEqual(f.ReasonCodes, []string{"SHIPMENT_SHORTFALL", "UNFULFILLED_RESERVATION"}) {
		t.Fatal("partial shipment explanation incorrect")
	}
	s.Shipments = append(s.Shipments, model.ShipmentRecord{ShipmentID: "orphan", OrderID: "unknown", SKUID: "s1", ShippedUnits: 2, ShippedAtISO: cutoff})
	s.Metadata.RecordCount = 2
	report = run(t, o, r, s)
	if report.Summary.Orphan != 1 || report.Findings[1].Classification != model.ClassificationOrphanEvidence || report.Findings[1].ShippedUnits != 2 {
		t.Fatal("unknown order shipment must be orphan evidence")
	}
}

func TestConflictExcludesEveryVariant(t *testing.T) {
	o, r, s := base()
	other := r.Events[0]
	other.ReservedUnits = 4
	r.Events = append(r.Events, other)
	r.Metadata.RecordCount = 2
	f := run(t, o, r, s).Findings[0]
	if f.Classification != model.ClassificationAmbiguousEvidence || f.ReservedUnits != 0 || f.ShippedUnits != 5 || f.QuarantineNote == "" {
		t.Fatalf("conflicting 5/4 must not select either quantity: %+v", f)
	}
	valid := r.Events[0]
	valid.EventID = "independent"
	valid.ReservedUnits = 2
	r.Events = append(r.Events, valid)
	r.Metadata.RecordCount = 3
	if f = run(t, o, r, s).Findings[0]; f.ReservedUnits != 2 || f.Classification != model.ClassificationAmbiguousEvidence {
		t.Fatal("unquarantined subtotal must include only independent evidence")
	}
}

func TestFullReportPermutationClockIndependenceAndNonmutation(t *testing.T) {
	o, r, s := base()
	o.Orders = append(o.Orders,
		model.OrderRecord{OrderID: "o2", SKUID: "s2", RequestedUnits: 5},
		model.OrderRecord{OrderID: "o3", SKUID: "s3", RequestedUnits: 5},
		model.OrderRecord{OrderID: "a:b", SKUID: "c", RequestedUnits: 1},
		model.OrderRecord{OrderID: "a", SKUID: "b:c", RequestedUnits: 1})
	o.Metadata.RecordCount = len(o.Orders)
	variant2, variant3 := r.Events[0], r.Events[0]
	variant2.OrderID, variant2.SKUID, variant2.ReservedUnits = "o2", "s2", 4
	variant3.OrderID, variant3.SKUID, variant3.ReservedUnits = "o3", "s3", 7
	r.Events = append(r.Events, variant2, variant3, r.Events[0], variant2)
	r.Metadata.RecordCount = len(r.Events)
	s.Shipments = append(s.Shipments, model.ShipmentRecord{ShipmentID: "x3", OrderID: "o2", SKUID: "s2", ShippedUnits: 5, ShippedAtISO: cutoff})
	s.Metadata.RecordCount = len(s.Shipments)
	before := encode(t, []any{o, r, s})
	first := run(t, o, r, s)
	if encode(t, []any{o, r, s}) != before {
		t.Fatal("caller input mutated")
	}
	if first.Summary.Ambiguous != 3 || first.Summary.Incomplete != 2 {
		t.Fatal("all three moved references must be quarantined")
	}
	for _, f := range first.Findings {
		if strings.HasPrefix(f.OrderID, "o") && (f.Classification != model.ClassificationAmbiguousEvidence || f.ReservedUnits != 0 || !reflect.DeepEqual(f.ReservationEventIDs, []string{"e1"})) {
			t.Fatal("variant reference not fully quarantined")
		}
	}
	expected := encode(t, first)
	rng := rand.New(rand.NewSource(97))
	for i := 0; i < 80; i++ {
		po := &model.OrderSnapshot{Metadata: o.Metadata, Orders: append([]model.OrderRecord{}, o.Orders...)}
		pr := &model.ReservationSnapshot{Metadata: r.Metadata, Events: append([]model.ReservationEvent{}, r.Events...)}
		ps := &model.ShipmentSnapshot{Metadata: s.Metadata, Shipments: append([]model.ShipmentRecord{}, s.Shipments...)}
		rng.Shuffle(len(po.Orders), func(i, j int) { po.Orders[i], po.Orders[j] = po.Orders[j], po.Orders[i] })
		rng.Shuffle(len(pr.Events), func(i, j int) { pr.Events[i], pr.Events[j] = pr.Events[j], pr.Events[i] })
		rng.Shuffle(len(ps.Shipments), func(i, j int) { ps.Shipments[i], ps.Shipments[j] = ps.Shipments[j], ps.Shipments[i] })
		permutationBefore := encode(t, []any{po, pr, ps})
		if got := encode(t, run(t, po, pr, ps)); got != expected {
			t.Fatal("complete serialized report changed under permutation or repetition")
		}
		if encode(t, []any{po, pr, ps}) != permutationBefore {
			t.Fatal("permuted input mutated")
		}
	}
	first.DeclaredCutoffs["orders"] = "changed"
	first.SourcesEvaluated[0] = "changed"
	first.Findings[len(first.Findings)-1].ReservationEventIDs[0] = "changed"
	first.Findings[0].ReasonCodes[0] = "changed"
	if encode(t, run(t, o, r, s)) != expected || encode(t, []any{o, r, s}) != before {
		t.Fatal("returned report aliases input or future results")
	}
}

func TestIncompletePrecedenceAndMissingRecords(t *testing.T) {
	o, r, s := base()
	for _, unavailable := range []string{"orders", "reservations", "shipments"} {
		po, pr, ps := o, r, s
		switch unavailable {
		case "orders":
			po = nil
		case "reservations":
			pr = nil
		case "shipments":
			ps = nil
		}
		report := run(t, po, pr, ps)
		if report.CutoffsAligned || report.Summary.Matched != 0 || report.Summary.Orphan != 0 || report.Summary.Incomplete != 1 {
			t.Fatal("unavailable source cannot prove a match or orphan")
		}
	}
	s.Metadata.CutoffISO = "2026-11-05T13:00:00Z"
	if report := run(t, o, r, s); report.Summary.Incomplete != 1 || report.CutoffsAligned {
		t.Fatal("cutoff mismatch must be incomplete")
	}
	conflict := r.Events[0]
	conflict.ReservedUnits = 4
	r.Events = append(r.Events, conflict)
	r.Metadata.RecordCount = 2
	f := run(t, o, r, s).Findings[0]
	if f.Classification != model.ClassificationIncompleteEvidence || f.QuarantineNote == "" || !reflect.DeepEqual(f.ReasonCodes, []string{"CONFLICTING_RESERVATION_EVENT", "CUTOFF_MISMATCH"}) {
		t.Fatal("global incomplete status must retain conflict evidence")
	}
	o, r, s = base()
	r.Events = nil
	r.Metadata.RecordCount = 0
	if run(t, o, r, s).Summary.Incomplete != 1 {
		t.Fatal("missing reservation row is not a numeric inconsistency")
	}
	o, r, s = base()
	s.Shipments = nil
	s.Metadata.RecordCount = 0
	if run(t, o, r, s).Summary.Incomplete != 1 {
		t.Fatal("missing shipment row is incomplete")
	}
	report := run(t, nil, nil, nil)
	if report.Summary.TotalOrderSKUPairs != 0 || report.CutoffsAligned || len(report.SystemWarnings) != 1 {
		t.Fatal("empty unavailable input needs global warning")
	}
}

func TestTypedBoundaryRejectsInvalidEvidence(t *testing.T) {
	mutations := []func(*model.OrderSnapshot, *model.ReservationSnapshot, *model.ShipmentSnapshot){
		func(o *model.OrderSnapshot, _ *model.ReservationSnapshot, _ *model.ShipmentSnapshot) {
			o.Orders[0].RequestedUnits = -1
		},
		func(_ *model.OrderSnapshot, r *model.ReservationSnapshot, _ *model.ShipmentSnapshot) {
			r.Events[0].ReservedUnits = -1
		},
		func(_ *model.OrderSnapshot, _ *model.ReservationSnapshot, s *model.ShipmentSnapshot) {
			s.Shipments[0].ShippedUnits = 0
		},
		func(o *model.OrderSnapshot, _ *model.ReservationSnapshot, _ *model.ShipmentSnapshot) {
			o.Orders = append(o.Orders, o.Orders[0])
			o.Metadata.RecordCount++
		},
		func(_ *model.OrderSnapshot, _ *model.ReservationSnapshot, s *model.ShipmentSnapshot) {
			s.Shipments[1].ShipmentID = s.Shipments[0].ShipmentID
		},
		func(o *model.OrderSnapshot, r *model.ReservationSnapshot, s *model.ShipmentSnapshot) {
			o.Metadata.CutoffISO = "equal-garbage"
			r.Metadata.CutoffISO = "equal-garbage"
			s.Metadata.CutoffISO = "equal-garbage"
		},
		func(o *model.OrderSnapshot, _ *model.ReservationSnapshot, _ *model.ShipmentSnapshot) {
			o.Metadata.RecordCount++
		},
		func(o *model.OrderSnapshot, _ *model.ReservationSnapshot, _ *model.ShipmentSnapshot) {
			o.Orders = make([]model.OrderRecord, 5001)
			o.Metadata.RecordCount = 5001
		},
		func(_ *model.OrderSnapshot, r *model.ReservationSnapshot, _ *model.ShipmentSnapshot) {
			r.Events[0].OccurredAtISO = "2026-11-05T12:00:01Z"
		},
	}
	for _, mutate := range mutations {
		o, r, s := base()
		mutate(o, r, s)
		if report, err := reconcile.Reconcile(o, r, s); err == nil || report != nil {
			t.Fatal("invalid typed input returned a report")
		}
	}
	// Individually bounded fields can still exceed the canonical source byte cap.
	o, r, s := base()
	r.Events = make([]model.ReservationEvent, 5000)
	r.Metadata.RecordCount = 5000
	for i := range r.Events {
		r.Events[i] = model.ReservationEvent{EventID: fmt.Sprintf("%0128d", i), OrderID: strings.Repeat("o", 128), SKUID: strings.Repeat("s", 128), ReservedUnits: 1, OccurredAtISO: cutoff}
	}
	if report, err := reconcile.Reconcile(o, r, s); err == nil || report != nil {
		t.Fatal("typed canonical payload size cap bypassed")
	}
}

func TestCheckedTotalsAtAndBeyondInt64Boundary(t *testing.T) {
	for _, source := range []string{"reservations", "shipments"} {
		o, r, s := base()
		if source == "shipments" {
			s.Shipments[0].ShippedUnits = math.MaxInt64 - 2
		} else {
			r.Events[0].ReservedUnits = math.MaxInt64 - 2
			extra := r.Events[0]
			extra.EventID = "e2"
			extra.ReservedUnits = 2
			r.Events = append(r.Events, extra)
			r.Metadata.RecordCount = 2
		}
		f := run(t, o, r, s).Findings[0]
		if source == "shipments" && f.ShippedUnits != math.MaxInt64 {
			t.Fatal("valid maximum shipment sum rejected")
		}
		if source == "reservations" && f.ReservedUnits != math.MaxInt64 {
			t.Fatal("valid maximum reservation sum rejected")
		}
		if source == "shipments" {
			s.Shipments[1].ShippedUnits = 3
		} else {
			r.Events[1].ReservedUnits = 3
		}
		if report, err := reconcile.Reconcile(o, r, s); err == nil || report != nil {
			t.Fatal("overflow must fail without partial report")
		}
	}
}

func TestFindingLimitIsExactFailureNotTruncation(t *testing.T) {
	o := &model.OrderSnapshot{Metadata: meta("orders", 5000), Orders: make([]model.OrderRecord, 5000)}
	r := &model.ReservationSnapshot{Metadata: meta("reservations", 5000), Events: make([]model.ReservationEvent, 5000)}
	s := &model.ShipmentSnapshot{Metadata: meta("shipments", 0)}
	for i := 0; i < 5000; i++ {
		o.Orders[i] = model.OrderRecord{OrderID: fmt.Sprintf("a%d", i), SKUID: "s", RequestedUnits: 1}
		r.Events[i] = model.ReservationEvent{EventID: fmt.Sprintf("e%d", i), OrderID: fmt.Sprintf("b%d", i), SKUID: "s", ReservedUnits: 1, OccurredAtISO: cutoff}
	}
	report := run(t, o, r, s)
	if len(report.Findings) != 10000 || report.Summary.TotalOrderSKUPairs != 10000 || report.Summary.Incomplete != 5000 || report.Summary.Orphan != 5000 {
		t.Fatal("exact limit report counts incorrect")
	}
	s.Shipments = []model.ShipmentRecord{{ShipmentID: "x", OrderID: "c", SKUID: "s", ShippedUnits: 1, ShippedAtISO: cutoff}}
	s.Metadata.RecordCount = 1
	if report, err := reconcile.Reconcile(o, r, s); err == nil || report != nil || err.Error() != "finding limit exceeded" {
		t.Fatal("over-limit evidence was silently truncated")
	}
}
