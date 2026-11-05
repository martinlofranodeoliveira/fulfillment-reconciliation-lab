package reconcile

import (
	"errors"
	"math"
	"sort"

	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/model"
)

type orderSKUPair struct{ orderID, skuID string }

type aggregate struct {
	requested, reserved, shipped int64
	hasOrder, conflict           bool
	eventIDs, shipmentIDs        []string
}

type eventGroup struct {
	first    model.ReservationEvent
	conflict bool
	pairs    map[orderSKUPair]struct{}
}

func add(total *int64, units int64) error {
	if units > math.MaxInt64-*total {
		return errors.New("checked unit addition overflow")
	}
	*total += units
	return nil
}

// Reconcile is clock-free and does not mutate or retain caller-owned containers.
// Nil snapshots mean unavailable sources, not empty successful snapshots.
func Reconcile(orders *model.OrderSnapshot, reservations *model.ReservationSnapshot, shipments *model.ShipmentSnapshot) (*model.ReconciliationReport, error) {
	if orders != nil {
		if err := orders.Validate(); err != nil {
			return nil, err
		}
	}
	if reservations != nil {
		if err := reservations.Validate(); err != nil {
			return nil, err
		}
	}
	if shipments != nil {
		if err := shipments.Validate(); err != nil {
			return nil, err
		}
	}

	sources := []string{}
	cutoffs := make(map[string]string)
	if orders != nil {
		sources = append(sources, "orders")
		cutoffs["orders"] = orders.Metadata.CutoffISO
	}
	if reservations != nil {
		sources = append(sources, "reservations")
		cutoffs["reservations"] = reservations.Metadata.CutoffISO
	}
	if shipments != nil {
		sources = append(sources, "shipments")
		cutoffs["shipments"] = shipments.Metadata.CutoffISO
	}
	missing := len(sources) != 3
	mismatch := false
	if len(sources) > 1 {
		for _, source := range sources[1:] {
			if cutoffs[source] != cutoffs[sources[0]] {
				mismatch = true
			}
		}
	}
	warnings := []string{}
	if missing {
		warnings = append(warnings, model.ReasonMissingSourceSnapshot)
	}
	if mismatch {
		warnings = append(warnings, model.ReasonCutoffMismatch)
	}
	sort.Strings(warnings)

	pairs := make(map[orderSKUPair]*aggregate)
	get := func(key orderSKUPair) (*aggregate, error) {
		if value, ok := pairs[key]; ok {
			return value, nil
		}
		if len(pairs) >= model.MaxOutputFindings {
			return nil, errors.New("finding limit exceeded")
		}
		value := &aggregate{eventIDs: []string{}, shipmentIDs: []string{}}
		pairs[key] = value
		return value, nil
	}
	if orders != nil {
		for _, order := range orders.Orders {
			value, err := get(orderSKUPair{order.OrderID, order.SKUID})
			if err != nil {
				return nil, err
			}
			value.requested, value.hasOrder = order.RequestedUnits, true
		}
	}
	groups := make(map[string]*eventGroup)
	if reservations != nil {
		for _, event := range reservations.Events {
			key := orderSKUPair{event.OrderID, event.SKUID}
			if _, err := get(key); err != nil {
				return nil, err
			}
			group, ok := groups[event.EventID]
			if !ok {
				group = &eventGroup{first: event, pairs: make(map[orderSKUPair]struct{})}
				groups[event.EventID] = group
			} else if group.first != event {
				group.conflict = true
			}
			group.pairs[key] = struct{}{}
		}
	}
	// Collect the entire bounded union before aggregation; never truncate output.
	if shipments != nil {
		for _, shipment := range shipments.Shipments {
			if _, err := get(orderSKUPair{shipment.OrderID, shipment.SKUID}); err != nil {
				return nil, err
			}
		}
	}
	for id, group := range groups {
		for key := range group.pairs {
			value := pairs[key]
			value.eventIDs = append(value.eventIDs, id)
			if group.conflict {
				value.conflict = true
			}
		}
		// No variant of a conflicting ID contributes to any numeric total.
		if !group.conflict {
			value := pairs[orderSKUPair{group.first.OrderID, group.first.SKUID}]
			if err := add(&value.reserved, group.first.ReservedUnits); err != nil {
				return nil, err
			}
		}
	}
	if shipments != nil {
		for _, shipment := range shipments.Shipments {
			value := pairs[orderSKUPair{shipment.OrderID, shipment.SKUID}]
			if err := add(&value.shipped, shipment.ShippedUnits); err != nil {
				return nil, err
			}
			value.shipmentIDs = append(value.shipmentIDs, shipment.ShipmentID)
		}
	}
	keys := make([]orderSKUPair, 0, len(pairs))
	for key := range pairs {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].orderID == keys[j].orderID {
			return keys[i].skuID < keys[j].skuID
		}
		return keys[i].orderID < keys[j].orderID
	})
	report := &model.ReconciliationReport{
		ReportVersion:    model.CurrentReportVersion,
		CutoffsAligned:   !missing && !mismatch,
		DeclaredCutoffs:  cutoffs,
		SourcesEvaluated: sources,
		Findings:         make([]model.ReconciliationFinding, 0, len(keys)),
		SystemWarnings:   warnings,
		Disclaimer:       "Notice: Only supplied synthetic snapshots are reconciled. Snapshot completeness is an input assumption; this report cannot certify physical inventory or detect actual lost goods.",
	}
	for _, key := range keys {
		value := pairs[key]
		sort.Strings(value.eventIDs)
		sort.Strings(value.shipmentIDs)
		finding := model.ReconciliationFinding{
			OrderID: key.orderID, SKUID: key.skuID,
			RequestedUnits: value.requested, ReservedUnits: value.reserved, ShippedUnits: value.shipped,
			DifferenceRequestedVsShipped: value.requested - value.shipped,
			DifferenceReservedVsShipped:  value.reserved - value.shipped,
			ReservationEventIDs:          value.eventIDs, ShipmentIDs: value.shipmentIDs,
			ReasonCodes: append([]string{}, warnings...),
		}
		if value.conflict {
			finding.ReasonCodes = append(finding.ReasonCodes, model.ReasonConflictingReservation)
			finding.QuarantineNote = "Conflicting reservation variants excluded; totals are unquarantined subtotals."
		}
		switch {
		case missing || mismatch:
			finding.Classification = model.ClassificationIncompleteEvidence
		case value.conflict:
			finding.Classification = model.ClassificationAmbiguousEvidence
		case !value.hasOrder:
			finding.Classification = model.ClassificationOrphanEvidence
			finding.ReasonCodes = append(finding.ReasonCodes, model.ReasonOrphanOrder)
		case len(value.eventIDs) == 0 || len(value.shipmentIDs) == 0:
			finding.Classification = model.ClassificationIncompleteEvidence
			if len(value.eventIDs) == 0 {
				finding.ReasonCodes = append(finding.ReasonCodes, model.ReasonMissingReservation)
			}
			if len(value.shipmentIDs) == 0 {
				finding.ReasonCodes = append(finding.ReasonCodes, model.ReasonMissingShipment)
			}
		case value.requested == value.reserved && value.reserved == value.shipped:
			finding.Classification = model.ClassificationMatched
			finding.ReasonCodes = append(finding.ReasonCodes, model.ReasonExactMatch)
		default:
			finding.Classification = model.ClassificationInconsistentQuantities
			if value.reserved < value.requested {
				finding.ReasonCodes = append(finding.ReasonCodes, model.ReasonReservationShortfall)
			}
			if value.reserved > value.requested {
				finding.ReasonCodes = append(finding.ReasonCodes, model.ReasonReservationSurplus)
			}
			if value.shipped < value.requested {
				finding.ReasonCodes = append(finding.ReasonCodes, model.ReasonShipmentShortfall)
			}
			if value.shipped > value.requested {
				finding.ReasonCodes = append(finding.ReasonCodes, model.ReasonShipmentSurplus)
			}
			if value.shipped < value.reserved {
				finding.ReasonCodes = append(finding.ReasonCodes, model.ReasonUnfulfilledReservation)
			}
			if value.shipped > value.reserved {
				finding.ReasonCodes = append(finding.ReasonCodes, model.ReasonOverFulfilledReservation)
			}
		}
		sort.Strings(finding.ReasonCodes)
		report.Findings = append(report.Findings, finding)
		switch finding.Classification {
		case model.ClassificationMatched:
			report.Summary.Matched++
		case model.ClassificationInconsistentQuantities:
			report.Summary.Inconsistent++
		case model.ClassificationAmbiguousEvidence:
			report.Summary.Ambiguous++
		case model.ClassificationIncompleteEvidence:
			report.Summary.Incomplete++
		case model.ClassificationOrphanEvidence:
			report.Summary.Orphan++
		}
	}
	report.Summary.TotalOrderSKUPairs = len(report.Findings)
	return report, nil
}
