package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
	"unicode/utf8"

	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/model"
)

const MaxNestingDepth = 8

func ReadBounded(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, errors.New("missing payload reader")
	}
	data, err := io.ReadAll(io.LimitReader(r, int64(model.MaxPayloadBytes)+1))
	if err != nil {
		return nil, errors.New("payload read failed")
	}
	if len(data) > model.MaxPayloadBytes {
		return nil, errors.New("payload length exceeds maximum limit")
	}
	return data, nil
}

// scan consumes one value without constructing its object/array value tree.
// Object key sets use decoded strings, including JSON escape equivalences.
func scan(dec *json.Decoder, depth int) error {
	token, err := dec.Token()
	if err != nil {
		return errors.New("malformed JSON")
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	depth++
	if depth > MaxNestingDepth {
		return errors.New("json nesting depth exceeds maximum limit")
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			token, err := dec.Token()
			if err != nil {
				return errors.New("malformed JSON")
			}
			key, ok := token.(string)
			if !ok || len(key) > model.MaxIDLength {
				return errors.New("invalid JSON object key")
			}
			if _, exists := seen[key]; exists {
				return errors.New("duplicate JSON key detected")
			}
			seen[key] = struct{}{}
			if err := scan(dec, depth); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("malformed JSON")
		}
	case '[':
		for dec.More() {
			if err := scan(dec, depth); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("malformed JSON")
		}
	default:
		return errors.New("malformed JSON")
	}
	return nil
}

func ValidateJSONStructure(data []byte) error {
	if len(data) > model.MaxPayloadBytes {
		return errors.New("payload length exceeds maximum limit")
	}
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8 JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := scan(dec, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing or malformed JSON data")
	}
	return nil
}

func integerToken(s string) bool {
	if s == "0" {
		return true
	}
	if len(s) == 0 || s[0] < '1' || s[0] > '9' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// All model fields are required and exact-case. This deliberately avoids the
// case-insensitive struct matching and null-to-zero behavior of encoding/json.
func checkShape(value any, typ reflect.Type) error {
	if value == nil {
		return errors.New("null is not permitted")
	}
	switch typ.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return errors.New("expected JSON object")
		}
		fields := make(map[string]reflect.Type, typ.NumField())
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			fields[f.Tag.Get("json")] = f.Type
		}
		for key := range object {
			if _, ok := fields[key]; !ok {
				return errors.New("unknown field")
			}
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			child, ok := object[f.Tag.Get("json")]
			if !ok {
				return errors.New("missing required field")
			}
			if err := checkShape(child, f.Type); err != nil {
				return err
			}
		}
	case reflect.Slice:
		array, ok := value.([]any)
		if !ok {
			return errors.New("expected JSON array")
		}
		if len(array) > model.MaxRecordsPerSource {
			return errors.New("record count exceeds maximum")
		}
		for _, child := range array {
			if err := checkShape(child, typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.String:
		if _, ok := value.(string); !ok {
			return errors.New("expected JSON string")
		}
	case reflect.Int, reflect.Int64:
		number, ok := value.(json.Number)
		if !ok || !integerToken(number.String()) {
			return errors.New("expected canonical nonnegative integer token")
		}
		if _, err := strconv.ParseInt(number.String(), 10, typ.Bits()); err != nil {
			return errors.New("integer exceeds supported range")
		}
	default:
		return errors.New("unsupported schema type")
	}
	return nil
}

func decode[T any](r io.Reader, validate func(*T) error) (*T, error) {
	data, err := ReadBounded(r)
	if err != nil {
		return nil, err
	}
	if err := ValidateJSONStructure(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, errors.New("schema decoding failed")
	}
	var result T
	if err := checkShape(value, reflect.TypeOf(result)); err != nil {
		return nil, err
	}
	dec = json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return nil, errors.New("schema decoding failed")
	}
	if err := validate(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func DecodeOrderSnapshot(r io.Reader) (*model.OrderSnapshot, error) {
	return decode(r, (*model.OrderSnapshot).Validate)
}

func DecodeReservationSnapshot(r io.Reader) (*model.ReservationSnapshot, error) {
	return decode(r, (*model.ReservationSnapshot).Validate)
}

func DecodeShipmentSnapshot(r io.Reader) (*model.ShipmentSnapshot, error) {
	return decode(r, (*model.ShipmentSnapshot).Validate)
}
