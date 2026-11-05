package model

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	MaxIDLength          = 128
	MaxRecordsPerSource  = 5000
	MaxOutputFindings    = 10000
	MaxPayloadBytes      = 2 * 1024 * 1024
	MaxReportBytes       = 16 * 1024 * 1024
	CurrentReportVersion = "0.1.0"
	CanonicalTimeLayout  = "2006-01-02T15:04:05Z"
)

type Classification string

const (
	ClassificationMatched                Classification = "MATCHED"
	ClassificationInconsistentQuantities Classification = "INCONSISTENT_QUANTITIES"
	ClassificationAmbiguousEvidence      Classification = "AMBIGUOUS_EVIDENCE"
	ClassificationIncompleteEvidence     Classification = "INCOMPLETE_EVIDENCE"
	ClassificationOrphanEvidence         Classification = "ORPHAN_EVIDENCE"
)

const (
	ReasonExactMatch               = "EXACT_MATCH"
	ReasonReservationShortfall     = "RESERVATION_SHORTFALL"
	ReasonReservationSurplus       = "RESERVATION_SURPLUS"
	ReasonShipmentShortfall        = "SHIPMENT_SHORTFALL"
	ReasonShipmentSurplus          = "SHIPMENT_SURPLUS"
	ReasonUnfulfilledReservation   = "UNFULFILLED_RESERVATION"
	ReasonOverFulfilledReservation = "OVER_FULFILLED_RESERVATION"
	ReasonConflictingReservation   = "CONFLICTING_RESERVATION_EVENT"
	ReasonCutoffMismatch           = "CUTOFF_MISMATCH"
	ReasonMissingSourceSnapshot    = "MISSING_SOURCE_SNAPSHOT"
	ReasonMissingReservation       = "MISSING_RESERVATION_RECORD"
	ReasonMissingShipment          = "MISSING_SHIPMENT_RECORD"
	ReasonOrphanOrder              = "ORPHAN_EVIDENCE_UNRECOGNIZED_ORDER"
)

type SnapshotMetadata struct {
	SchemaVersion string `json:"schema_version"`
	Source        string `json:"source"`
	SnapshotID    string `json:"snapshot_id"`
	CutoffISO     string `json:"cutoff_iso"`
	RecordCount   int    `json:"record_count"`
}

type OrderRecord struct {
	OrderID        string `json:"order_id"`
	SKUID          string `json:"sku_id"`
	RequestedUnits int64  `json:"requested_units"`
}

type OrderSnapshot struct {
	Metadata SnapshotMetadata `json:"metadata"`
	Orders   []OrderRecord    `json:"orders"`
}

type ReservationEvent struct {
	EventID       string `json:"event_id"`
	OrderID       string `json:"order_id"`
	SKUID         string `json:"sku_id"`
	ReservedUnits int64  `json:"reserved_units"`
	OccurredAtISO string `json:"occurred_at_iso"`
}

type ReservationSnapshot struct {
	Metadata SnapshotMetadata   `json:"metadata"`
	Events   []ReservationEvent `json:"events"`
}

type ShipmentRecord struct {
	ShipmentID   string `json:"shipment_id"`
	OrderID      string `json:"order_id"`
	SKUID        string `json:"sku_id"`
	ShippedUnits int64  `json:"shipped_units"`
	ShippedAtISO string `json:"shipped_at_iso"`
}

type ShipmentSnapshot struct {
	Metadata  SnapshotMetadata `json:"metadata"`
	Shipments []ShipmentRecord `json:"shipments"`
}

type ReconciliationFinding struct {
	OrderID                      string         `json:"order_id"`
	SKUID                        string         `json:"sku_id"`
	Classification               Classification `json:"classification"`
	RequestedUnits               int64          `json:"requested_units"`
	ReservedUnits                int64          `json:"reserved_units"`
	ShippedUnits                 int64          `json:"shipped_units"`
	DifferenceRequestedVsShipped int64          `json:"difference_requested_vs_shipped"`
	DifferenceReservedVsShipped  int64          `json:"difference_reserved_vs_shipped"`
	ReasonCodes                  []string       `json:"reason_codes"`
	ReservationEventIDs          []string       `json:"reservation_event_ids"`
	ShipmentIDs                  []string       `json:"shipment_ids"`
	QuarantineNote               string         `json:"quarantine_note,omitempty"`
}

type ReportSummary struct {
	TotalOrderSKUPairs int `json:"total_order_sku_pairs"`
	Matched            int `json:"matched"`
	Inconsistent       int `json:"inconsistent"`
	Ambiguous          int `json:"ambiguous"`
	Incomplete         int `json:"incomplete"`
	Orphan             int `json:"orphan"`
}

type ReconciliationReport struct {
	ReportVersion    string                  `json:"report_version"`
	CutoffsAligned   bool                    `json:"cutoffs_aligned"`
	DeclaredCutoffs  map[string]string       `json:"declared_cutoffs"`
	SourcesEvaluated []string                `json:"sources_evaluated"`
	Summary          ReportSummary           `json:"summary"`
	Findings         []ReconciliationFinding `json:"findings"`
	SystemWarnings   []string                `json:"system_warnings,omitempty"`
	Disclaimer       string                  `json:"disclaimer"`
}

// ValidateBusinessID retains the field argument for callers, but never echoes it.
func ValidateBusinessID(_ string, value string) error {
	if len(value) == 0 || len(value) > MaxIDLength {
		return errors.New("invalid business identifier length")
	}
	for _, c := range []byte(value) {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' || c == ':') {
			return errors.New("invalid business identifier characters")
		}
	}
	return nil
}

func validTime(value string) bool {
	if len(value) != 20 {
		return false
	}
	t, err := time.Parse(CanonicalTimeLayout, value)
	return err == nil && t.Year() >= 1 && t.Format(CanonicalTimeLayout) == value
}

func validateMetadata(m SnapshotMetadata, source string, count int) error {
	if count > MaxRecordsPerSource {
		return errors.New("record count exceeds maximum")
	}
	if m.SchemaVersion != CurrentReportVersion {
		return errors.New("unsupported snapshot schema version")
	}
	if m.Source != source {
		return errors.New("invalid metadata source")
	}
	if err := ValidateBusinessID("snapshot_id", m.SnapshotID); err != nil {
		return err
	}
	if !validTime(m.CutoffISO) {
		return errors.New("invalid canonical UTC cutoff")
	}
	if m.RecordCount != count {
		return errors.New("record_count does not equal array length")
	}
	return nil
}

// Bounds on fields and record counts are checked before this bounded allocation.
func validateEncodedSize(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("snapshot encoding failed")
	}
	if len(data) > MaxPayloadBytes {
		return errors.New("payload length exceeds maximum limit")
	}
	return nil
}

func (s *OrderSnapshot) Validate() error {
	if s == nil {
		return errors.New("nil snapshot")
	}
	if err := validateMetadata(s.Metadata, "orders", len(s.Orders)); err != nil {
		return err
	}
	type pair struct{ order, sku string }
	seen := make(map[pair]struct{}, len(s.Orders))
	for _, record := range s.Orders {
		if err := ValidateBusinessID("order_id", record.OrderID); err != nil {
			return err
		}
		if err := ValidateBusinessID("sku_id", record.SKUID); err != nil {
			return err
		}
		if record.RequestedUnits <= 0 {
			return errors.New("requested_units must be positive")
		}
		key := pair{record.OrderID, record.SKUID}
		if _, ok := seen[key]; ok {
			return errors.New("duplicate order/SKU pair")
		}
		seen[key] = struct{}{}
	}
	return validateEncodedSize(s)
}

func (s *ReservationSnapshot) Validate() error {
	if s == nil {
		return errors.New("nil snapshot")
	}
	if err := validateMetadata(s.Metadata, "reservations", len(s.Events)); err != nil {
		return err
	}
	for _, event := range s.Events {
		for _, id := range []string{event.EventID, event.OrderID, event.SKUID} {
			if err := ValidateBusinessID("id", id); err != nil {
				return err
			}
		}
		if event.ReservedUnits <= 0 {
			return errors.New("reserved_units must be positive")
		}
		if !validTime(event.OccurredAtISO) || event.OccurredAtISO > s.Metadata.CutoffISO {
			return errors.New("invalid reservation time or time after cutoff")
		}
	}
	return validateEncodedSize(s)
}

func (s *ShipmentSnapshot) Validate() error {
	if s == nil {
		return errors.New("nil snapshot")
	}
	if err := validateMetadata(s.Metadata, "shipments", len(s.Shipments)); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(s.Shipments))
	for _, shipment := range s.Shipments {
		for _, id := range []string{shipment.ShipmentID, shipment.OrderID, shipment.SKUID} {
			if err := ValidateBusinessID("id", id); err != nil {
				return err
			}
		}
		if shipment.ShippedUnits <= 0 {
			return errors.New("shipped_units must be positive")
		}
		if !validTime(shipment.ShippedAtISO) || shipment.ShippedAtISO > s.Metadata.CutoffISO {
			return errors.New("invalid shipment time or time after cutoff")
		}
		if _, ok := seen[shipment.ShipmentID]; ok {
			return errors.New("duplicate shipment ID")
		}
		seen[shipment.ShipmentID] = struct{}{}
	}
	return validateEncodedSize(s)
}
