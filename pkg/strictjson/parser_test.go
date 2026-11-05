package strictjson_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/model"
	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/strictjson"
)

const validOrder = `{"metadata":{"schema_version":"0.1.0","source":"orders","snapshot_id":"snap","cutoff_iso":"2026-11-05T12:00:00Z","record_count":1},"orders":[{"order_id":"o1","sku_id":"s1","requested_units":5}]}`

func TestCanonicalIntegerTokens(t *testing.T) {
	for _, value := range []string{"5.5", "5.0", "5e0", "5E+0", "-0", "-1", "0", "true", "false", `"5"`, "null", "9223372036854775808", strings.Repeat("9", 500)} {
		payload := strings.Replace(validOrder, `"requested_units":5`, `"requested_units":`+value, 1)
		if _, err := strictjson.DecodeOrderSnapshot(strings.NewReader(payload)); err == nil {
			t.Fatalf("invalid units accepted for token category %q", value)
		}
	}
	payload := strings.Replace(validOrder, `"requested_units":5`, `"requested_units":9223372036854775807`, 1)
	s, err := strictjson.DecodeOrderSnapshot(strings.NewReader(payload))
	if err != nil || s.Orders[0].RequestedUnits != 9223372036854775807 {
		t.Fatalf("int64 boundary was not preserved: %v", err)
	}
	for _, count := range []string{"1.0", "1e0", "true", "null", "-0"} {
		payload := strings.Replace(validOrder, `"record_count":1`, `"record_count":`+count, 1)
		if _, err := strictjson.DecodeOrderSnapshot(strings.NewReader(payload)); err == nil {
			t.Fatal("noncanonical record count accepted")
		}
	}
}

func TestDecodedDuplicateKeysAtEveryLevel(t *testing.T) {
	for _, payload := range []string{
		`{"secret":1,"\u0073ecret":2}`,
		`{"a":[{"secret":1,"secret":2}]}`,
		strings.Replace(validOrder, `"source":"orders"`, `"source":"orders","\u0073ource":"orders"`, 1),
		strings.Replace(validOrder, `"order_id":"o1"`, `"order_id":"o1","order_id":"o2"`, 1),
		strings.Replace(validOrder, `"orders":[`, `"orders":[],"orders":[`, 1),
	} {
		err := strictjson.ValidateJSONStructure([]byte(payload))
		if err == nil || err.Error() != "duplicate JSON key detected" {
			t.Fatalf("decoded duplicate key not rejected safely: %v", err)
		}
	}
}

func TestStructureBoundsAndMalformedInputs(t *testing.T) {
	if err := strictjson.ValidateJSONStructure([]byte(strings.Repeat("[", 8) + "0" + strings.Repeat("]", 8))); err != nil {
		t.Fatalf("depth eight should pass structural validation: %v", err)
	}
	for _, payload := range [][]byte{
		[]byte(strings.Repeat("[", 9) + "0" + strings.Repeat("]", 9)),
		[]byte(""), []byte("  "), []byte(`{"a":`), []byte(`[] []`), []byte(`{} secret`),
		[]byte("{\"a\":\"\xff\"}"), bytes.Repeat([]byte(" "), model.MaxPayloadBytes+1),
	} {
		if strictjson.ValidateJSONStructure(payload) == nil {
			t.Fatal("malformed or excessive structure accepted")
		}
	}
}

func TestRequiredExactCaseNonNullSchema(t *testing.T) {
	var base map[string]any
	if err := json.Unmarshal([]byte(validOrder), &base); err != nil {
		t.Fatal(err)
	}
	mutations := []func(map[string]any){
		func(v map[string]any) { v["private-payload-key"] = "private-payload-value" },
		func(v map[string]any) { v["Metadata"] = v["metadata"]; delete(v, "metadata") },
		func(v map[string]any) { v["metadata"] = nil },
		func(v map[string]any) { v["orders"] = nil },
		func(v map[string]any) { delete(v, "orders") },
		func(v map[string]any) { delete(v["metadata"].(map[string]any), "record_count") },
		func(v map[string]any) { v["metadata"].(map[string]any)["record_count"] = 0 },
		func(v map[string]any) { v["metadata"].(map[string]any)["source"] = "private-payload-value" },
		func(v map[string]any) { v["metadata"].(map[string]any)["cutoff_iso"] = "private-payload-value" },
		func(v map[string]any) { v["orders"].([]any)[0] = nil },
		func(v map[string]any) { v["orders"].([]any)[0].(map[string]any)["requested_units"] = nil },
		func(v map[string]any) { delete(v["orders"].([]any)[0].(map[string]any), "sku_id") },
	}
	for i, mutate := range mutations {
		var value map[string]any
		if err := json.Unmarshal([]byte(validOrder), &value); err != nil {
			t.Fatal(err)
		}
		mutate(value)
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		_, err = strictjson.DecodeOrderSnapshot(bytes.NewReader(data))
		if err == nil || strings.Contains(err.Error(), "private-payload") {
			t.Fatalf("mutation %d accepted or leaked payload: %v", i, err)
		}
	}
	for _, scalar := range []string{"null", "true", "[]", "5"} {
		if _, err := strictjson.DecodeOrderSnapshot(strings.NewReader(scalar)); err == nil {
			t.Fatal("nonobject snapshot accepted")
		}
	}
}

func TestIdentifierAndRecordLimits(t *testing.T) {
	for _, n := range []int{128, 129} {
		payload := strings.Replace(validOrder, `"o1"`, `"`+strings.Repeat("a", n)+`"`, 1)
		_, err := strictjson.DecodeOrderSnapshot(strings.NewReader(payload))
		if (err == nil) != (n == 128) {
			t.Fatal("identifier boundary failed")
		}
	}
	payload := `{"metadata":{"schema_version":"0.1.0","source":"orders","snapshot_id":"s","cutoff_iso":"2026-11-05T12:00:00Z","record_count":5001},"orders":[` + strings.Repeat(`{},`, 5000) + `{}]}`
	if _, err := strictjson.DecodeOrderSnapshot(strings.NewReader(payload)); err == nil {
		t.Fatal("5001 records accepted")
	}
	duplicate := strings.Replace(validOrder, `"record_count":1`, `"record_count":2`, 1)
	duplicate = strings.Replace(duplicate, `}]}`, `},{"order_id":"o1","sku_id":"s1","requested_units":5}]}`, 1)
	if _, err := strictjson.DecodeOrderSnapshot(strings.NewReader(duplicate)); err == nil {
		t.Fatal("duplicate order pair accepted through JSON")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("private-reader-data") }

type countingReader struct{ count int }

func (r *countingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	r.count += len(p)
	return len(p), nil
}

func TestBoundedReadAndSanitizedReadError(t *testing.T) {
	r := &countingReader{}
	if _, err := strictjson.ReadBounded(r); err == nil || r.count != model.MaxPayloadBytes+1 {
		t.Fatalf("reader was not bounded: bytes=%d error=%v", r.count, err)
	}
	if _, err := strictjson.ReadBounded(failingReader{}); err == nil || strings.Contains(err.Error(), "private-reader") {
		t.Fatal("native reader error leaked")
	}
	data, err := strictjson.ReadBounded(io.LimitReader(&countingReader{}, model.MaxPayloadBytes))
	if err != nil || len(data) != model.MaxPayloadBytes {
		t.Fatal("exact byte limit rejected")
	}
}

func TestCommittedFixtures(t *testing.T) {
	for _, name := range []string{"invalid_duplicate_keys.json", "invalid_float_quantity.json"} {
		data, err := os.ReadFile("../../testdata/fixtures/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := strictjson.DecodeOrderSnapshot(bytes.NewReader(data)); err == nil {
			t.Fatal("invalid committed fixture accepted")
		}
	}
	for name, decode := range map[string]func(io.Reader) error{
		"orders_aligned.json":            func(r io.Reader) error { _, e := strictjson.DecodeOrderSnapshot(r); return e },
		"reservations_aligned.json":      func(r io.Reader) error { _, e := strictjson.DecodeReservationSnapshot(r); return e },
		"reservations_conflicting.json":  func(r io.Reader) error { _, e := strictjson.DecodeReservationSnapshot(r); return e },
		"shipments_aligned.json":         func(r io.Reader) error { _, e := strictjson.DecodeShipmentSnapshot(r); return e },
		"shipments_cutoff_mismatch.json": func(r io.Reader) error { _, e := strictjson.DecodeShipmentSnapshot(r); return e },
	} {
		data, err := os.ReadFile("../../testdata/fixtures/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := decode(bytes.NewReader(data)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
