package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/client"
	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/fixture"
	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/model"
	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/reconcile"
)

type demoScenario struct {
	Name   string                      `json:"name"`
	Report *model.ReconciliationReport `json:"report"`
}
type demoResult struct {
	Version   string           `json:"version"`
	Scenarios []demoScenario   `json:"scenarios"`
	Requests  map[string]int64 `json:"fixture_requests"`
	Failure   string           `json:"source_failure"`
}

func demoMetadata(source string, count int) model.SnapshotMetadata {
	return model.SnapshotMetadata{SchemaVersion: "0.1.0", Source: source, SnapshotID: "demo-" + source, CutoffISO: "2026-11-05T12:00:00Z", RecordCount: count}
}

// buildDemo returns only after deferred client/server cleanup has completed.
func buildDemo() (*demoResult, error) {
	orders := model.OrderSnapshot{Metadata: demoMetadata("orders", 3), Orders: []model.OrderRecord{
		{OrderID: "o1", SKUID: "s1", RequestedUnits: 5},
		{OrderID: "o2", SKUID: "s2", RequestedUnits: 5},
		{OrderID: "o3", SKUID: "s3", RequestedUnits: 5},
	}}
	reservations := model.ReservationSnapshot{Metadata: demoMetadata("reservations", 5), Events: []model.ReservationEvent{
		{EventID: "e1", OrderID: "o1", SKUID: "s1", ReservedUnits: 5, OccurredAtISO: "2026-11-05T10:00:00Z"},
		{EventID: "e1", OrderID: "o1", SKUID: "s1", ReservedUnits: 5, OccurredAtISO: "2026-11-05T10:00:00Z"},
		{EventID: "e2", OrderID: "o2", SKUID: "s2", ReservedUnits: 5, OccurredAtISO: "2026-11-05T10:00:00Z"},
		{EventID: "e2", OrderID: "o2", SKUID: "s2", ReservedUnits: 4, OccurredAtISO: "2026-11-05T10:00:00Z"},
		{EventID: "e3", OrderID: "o3", SKUID: "s3", ReservedUnits: 5, OccurredAtISO: "2026-11-05T10:00:00Z"},
	}}
	shipments := model.ShipmentSnapshot{Metadata: demoMetadata("shipments", 5), Shipments: []model.ShipmentRecord{
		{ShipmentID: "x1", OrderID: "o1", SKUID: "s1", ShippedUnits: 3, ShippedAtISO: "2026-11-05T11:00:00Z"},
		{ShipmentID: "x2", OrderID: "o1", SKUID: "s1", ShippedUnits: 2, ShippedAtISO: "2026-11-05T11:00:00Z"},
		{ShipmentID: "x3", OrderID: "o2", SKUID: "s2", ShippedUnits: 5, ShippedAtISO: "2026-11-05T11:00:00Z"},
		{ShipmentID: "x4", OrderID: "o3", SKUID: "s3", ShippedUnits: 3, ShippedAtISO: "2026-11-05T11:00:00Z"},
		{ShipmentID: "x5", OrderID: "o999", SKUID: "s999", ShippedUnits: 2, ShippedAtISO: "2026-11-05T11:00:00Z"},
	}}
	start := func(value any) (*fixture.Server, error) {
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, errors.New("demo fixture encoding failed")
		}
		return fixture.NewServer(payload)
	}
	oServer, err := start(orders)
	if err != nil {
		return nil, err
	}
	defer oServer.Close()
	rServer, err := start(reservations)
	if err != nil {
		return nil, err
	}
	defer rServer.Close()
	sServer, err := start(shipments)
	if err != nil {
		return nil, err
	}
	defer sServer.Close()
	c := client.NewLoopbackClient(2 * time.Second)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	o, err := c.FetchOrderSnapshot(ctx, oServer.URL())
	if err != nil {
		return nil, err
	}
	r, err := c.FetchReservationSnapshot(ctx, rServer.URL())
	if err != nil {
		return nil, err
	}
	s, err := c.FetchShipmentSnapshot(ctx, sServer.URL())
	if err != nil {
		return nil, err
	}
	baseline, err := reconcile.Reconcile(o, r, s)
	if err != nil {
		return nil, err
	}
	// Independent, hand-derived oracles; not expectations computed by the engine.
	expected := []struct {
		order, sku                              string
		class                                   model.Classification
		requested, reserved, shipped, remaining int64
	}{
		{"o1", "s1", model.ClassificationMatched, 5, 5, 5, 0},
		{"o2", "s2", model.ClassificationAmbiguousEvidence, 5, 0, 5, 0},
		{"o3", "s3", model.ClassificationInconsistentQuantities, 5, 5, 3, 2},
		{"o999", "s999", model.ClassificationOrphanEvidence, 0, 0, 2, -2},
	}
	if len(baseline.Findings) != len(expected) {
		return nil, errors.New("demo finding-count assertion failed")
	}
	for i, want := range expected {
		got := baseline.Findings[i]
		if got.OrderID != want.order || got.SKUID != want.sku || got.Classification != want.class || got.RequestedUnits != want.requested || got.ReservedUnits != want.reserved || got.ShippedUnits != want.shipped || got.DifferenceRequestedVsShipped != want.remaining {
			return nil, errors.New("demo quantity/classification assertion failed")
		}
	}
	if oServer.RequestCount() != 1 || rServer.RequestCount() != 1 || sServer.RequestCount() != 1 {
		return nil, errors.New("demo HTTP request assertion failed")
	}
	moved := *s
	moved.Metadata.CutoffISO = "2026-11-05T13:00:00Z"
	mismatch, err := reconcile.Reconcile(o, r, &moved)
	if err != nil {
		return nil, err
	}
	if mismatch.CutoffsAligned || mismatch.Summary.Incomplete != 4 || mismatch.Summary.Matched != 0 {
		return nil, errors.New("demo cutoff assertion failed")
	}
	if err := sServer.SetResponse(500, []byte("synthetic fixture fault")); err != nil {
		return nil, err
	}
	_, failure := c.FetchShipmentSnapshot(ctx, sServer.URL())
	if failure == nil || failure.Error() != "server returned unexpected status code 500" {
		return nil, errors.New("demo failure assertion failed")
	}
	unavailable, err := reconcile.Reconcile(o, r, nil)
	if err != nil {
		return nil, err
	}
	if unavailable.Summary.Incomplete != 3 || unavailable.Summary.Matched != 0 || unavailable.Summary.Orphan != 0 {
		return nil, errors.New("demo unavailable-source assertion failed")
	}
	return &demoResult{
		Version:   "0.1.0",
		Scenarios: []demoScenario{{"duplicates_conflict_partial_orphan", baseline}, {"cutoff_mismatch", mismatch}, {"source_failure", unavailable}},
		Requests:  map[string]int64{"orders": oServer.RequestCount(), "reservations": rServer.RequestCount(), "shipments": sServer.RequestCount()},
		Failure:   failure.Error(),
	}, nil
}

func runDemo(w io.Writer) error { return runDemoFormat(w, "text") }
func runDemoFormat(w io.Writer, format string) error {
	result, err := buildDemo()
	if err != nil {
		return err
	}
	data, err := render(format, result, func(out io.Writer) error {
		for _, scenario := range result.Scenarios {
			if _, err := fmt.Fprintln(out, "scenario="+scenario.Name); err != nil {
				return err
			}
			if err := formatTextReport(out, scenario.Report); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(out, "source_failure=%s\nfixture_requests orders=%d reservations=%d shipments=%d\nDemo assertions verified; fixtures closed.\n", result.Failure, result.Requests["orders"], result.Requests["reservations"], result.Requests["shipments"])
		return err
	})
	if err != nil {
		return err
	}
	return writeOutput(w, data)
}
