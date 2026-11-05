# Contract — development 0.1.0

This is the narrow contract of Fulfillment Reconciliation Lab, not a general JSON
schema processor or an inventory completeness certification. All inputs are
synthetic. Contract errors return no report. Diagnostics do not include raw input
keys, IDs, quantities, timestamps, paths, response bodies or native network errors.
Business identifiers are intentionally exported in successful reports.

## Public API

Module: `github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab`.

- `strictjson.DecodeOrderSnapshot(io.Reader)` returns `(*model.OrderSnapshot, error)`.
- `strictjson.DecodeReservationSnapshot(io.Reader)` returns `(*model.ReservationSnapshot, error)`.
- `strictjson.DecodeShipmentSnapshot(io.Reader)` returns `(*model.ShipmentSnapshot, error)`.
- `reconcile.Reconcile(*model.OrderSnapshot, *model.ReservationSnapshot,
  *model.ShipmentSnapshot)` returns `(*model.ReconciliationReport, error)`.
- Snapshot `Validate()` methods enforce typed semantic/size constraints.
- `client.NewLoopbackClient(time.Duration)` creates a fixture reader. Fetch methods
  take a context and explicit URL. Call `Close()` after completing requests.
- `fixture.NewServer([]byte)` starts a synthetic loopback fixture.
  `SetResponse(int, []byte) error` replaces its response; `RequestCount()` exposes
  actual valid GET request count. `Close() error` is idempotent and waits for the
  serving goroutine and started handlers.

Reconciliation is clock-free, has no I/O, does not mutate inputs and retains no
caller-owned containers. Returned maps/slices are fresh. Reports are byte-identical
when serialized with the same encoder across repeated/permuted valid inputs.
Callers must not concurrently mutate snapshots or byte slices during API calls.
A nil source pointer means unavailable. Typed nil record slices mean empty arrays;
JSON must explicitly provide arrays, never null. There is no generated timestamp.

## Exact snapshot schema

Every shown property is required. Names are case-sensitive. No unknown properties,
nulls, duplicate decoded keys (including escape-equivalent names) or trailing JSON
values are allowed. Input bytes must be UTF-8. Root objects and record arrays are
mandatory. Version `0.1.0` is the only supported schema version.

Every snapshot has this metadata, with its own source and raw array length:

```json
{
  "schema_version": "0.1.0",
  "source": "orders",
  "snapshot_id": "synthetic-snapshot",
  "cutoff_iso": "2026-11-05T12:00:00Z",
  "record_count": 1
}
```

The complete orders example:

```json
{
  "metadata": {
    "schema_version": "0.1.0",
    "source": "orders",
    "snapshot_id": "synthetic-orders",
    "cutoff_iso": "2026-11-05T12:00:00Z",
    "record_count": 1
  },
  "orders": [
    {"order_id": "o1", "sku_id": "s1", "requested_units": 5}
  ]
}
```

Reservations use top-level `metadata` and `events`; `metadata.source` is
`reservations`. Each event has exactly:

```json
{"event_id":"e1","order_id":"o1","sku_id":"s1","reserved_units":5,"occurred_at_iso":"2026-11-05T10:00:00Z"}
```

Shipments use top-level `metadata` and `shipments`; `metadata.source` is `shipments`.
Each record has exactly:

```json
{"shipment_id":"x1","order_id":"o1","sku_id":"s1","shipped_units":5,"shipped_at_iso":"2026-11-05T11:00:00Z"}
```

### Bounds and lexical rules

| Item | Rule |
| --- | --- |
| Source payload | At most 2,097,152 bytes, including whitespace |
| Typed source size | Compact `encoding/json` serialization at most 2,097,152 bytes |
| Record array | At most 5,000 entries, including repeated events |
| `record_count` | Exact raw array length, 0–5,000 |
| Container depth | At most 8, root object/array counts as 1 |
| IDs, including snapshot ID | 1–128 ASCII characters from `A-Z a-z 0-9 _ - . :` |
| Units | 1 through 9,223,372,036,854,775,807, represented by int64 |
| Integer JSON spelling | `0` or a nonzero decimal digit followed by decimal digits; units cannot be 0 |
| Timestamps | Exactly `YYYY-MM-DDTHH:MM:SSZ`, valid Gregorian date, year 0001–9999 |
| Evidence times | At or before their own snapshot cutoff |
| Findings | At most 10,000 distinct exact order/SKU tuples; excess is an error |
| CLI rendering | At most 16,777,216 bytes per invocation |

Fractions, exponent notation, signed tokens including `-0`, strings and booleans
are not integer values under this contract. Timestamp offsets, fractional seconds
and leap-second spelling are not supported. Equal canonical UTC cutoff strings
are required for aligned evidence. Snapshot IDs need not agree across sources;
they identify independently supplied exports.

Depth/decoded-key validation precedes materializing the JSON value tree. The
parser separately enforces exact required properties, lexical numbers, int64
range and semantics; `UseNumber`/`DisallowUnknownFields` alone do not do these jobs.
Typed validation checks record/field bounds before bounded serialization.

## Duplicate and arithmetic policies

- Orders: each `(order_id, sku_id)` tuple must be unique, even for identical rows.
  The order ID alone may repeat for different SKUs. Tuples are not joined strings.
- Shipments: `shipment_id` is globally unique within that supplied source.
  Any repeat, identical or conflicting, rejects the source. Split lines need
  different shipment IDs and are summed by exact order/SKU tuple.
- Reservations: `event_id` groups deliveries within the source. Identical decoded
  records count once. Equality includes order, SKU, units and occurrence time.
  If any variant differs, **all variants** of that event ID are excluded from all
  totals; **every referenced pair** is quarantined. No first/last variant is chosen.
  Independent, nonconflicting IDs still contribute unquarantined subtotals.
- All reservation and shipment additions check the positive int64 limit before
  addition. Any overflow fails the whole operation, even for an otherwise
  incomplete or quarantined pair. No report is truncated or partially returned.

## Classification precedence

For every observed tuple, in this exact order:

1. Any unavailable source or any disagreement among supplied cutoffs:
   `INCOMPLETE_EVIDENCE`. Reasons include `MISSING_SOURCE_SNAPSHOT` and/or
   `CUTOFF_MISMATCH`. A conflict also retains `CONFLICTING_RESERVATION_EVENT` and
   its quarantine note. Missing order source cannot establish an orphan.
2. With all sources aligned, a conflicting reservation ID touching the tuple:
   `AMBIGUOUS_EVIDENCE`, reason `CONFLICTING_RESERVATION_EVENT`.
3. With aligned sources and no conflict, no requested order/SKU record:
   `ORPHAN_EVIDENCE`, reason `ORPHAN_EVIDENCE_UNRECOGNIZED_ORDER`. This denotes a
   missing supplied tuple, not proof that a real order does not exist.
4. For a supplied requested tuple, no reservation record or no shipment record:
   `INCOMPLETE_EVIDENCE`, reasons `MISSING_RESERVATION_RECORD` and/or
   `MISSING_SHIPMENT_RECORD`. Empty evidence is not an asserted quantity of zero.
5. Requested = reserved = shipped: `MATCHED`, reason `EXACT_MATCH`.
6. Otherwise: `INCONSISTENT_QUANTITIES`. Compare reserved with requested for
   `RESERVATION_SHORTFALL`/`RESERVATION_SURPLUS`; shipped with requested for
   `SHIPMENT_SHORTFALL`/`SHIPMENT_SURPLUS`; shipped with reserved for
   `UNFULFILLED_RESERVATION`/`OVER_FULFILLED_RESERVATION`. Include every applicable
   reason.

Thus requested 5, reserved 5, shipments 3+2 yields matched shipped 5. Replaying the
same reservation leaves totals unchanged. A sole partial shipment of 3 yields
remaining 2 and inconsistency, not a conclusion about physical loss.

## Report fields and ordering

`report_version` is `0.1.0`; `cutoffs_aligned` requires all three sources and equal
cutoffs. `declared_cutoffs` contains available sources only. `sources_evaluated`
is the available subsequence of orders, reservations, shipments. `summary` counts
all findings exactly, in disjoint classification buckets. `findings` is sorted by
ASCII order ID then SKU ID, with empty arrays encoded as arrays, not null.
`system_warnings`, when present, lists sorted global missing-source/cutoff reason
codes, including when there are no findings. `disclaimer` states the evidence limit.

Each finding exports IDs, classification, requested/reserved/shipped units,
`difference_requested_vs_shipped` and `difference_reserved_vs_shipped` (signed
subtractions, not clamped), sorted unique `reservation_event_ids`, sorted
`shipment_ids`, sorted `reason_codes` and an optional fixed `quarantine_note`.
Zero totals can mean unavailable/excluded evidence. Differences are arithmetic
only; ambiguous/incomplete totals are not authoritative fulfillment conclusions.
Quarantined reservation IDs are included in evidence lists for every touched pair.

## I/O and CLI

The fixture URL subset is exactly `http://127.0.0.1:<port>/snapshot`, port 1–65535
in ordinary decimal spelling. No hostname/DNS, other address, HTTPS, userinfo,
encoded path, query or fragment is accepted. The dialer independently checks the
literal address. Proxies, redirects and decompression are disabled. Total timeout
is default 2 seconds and clamped to at most 5 seconds; contexts may shorten it.
Response headers are capped at 16 KiB and body reads at 2 MiB plus one detection
byte. Non-200 bodies are not surfaced in errors. Header/status framing is also
subject to Go's HTTP parser. `Close()` releases idle connections.

Fixtures own immutable copies, allow at most eight active connections, have 16 KiB
header configuration and 1-second header/idle, 2-second read/write deadlines.
Payload storage permits at most 2 MiB plus one byte specifically to test client
oversize rejection. Fixture methods/statuses are not a production service protocol.

CLI `reconcile` accepts explicit local regular nonsymlink files only. Linux/POSIX
nonblocking/no-follow open and descriptor identity/type checks reject FIFOs and
replacement races. No implicit file discovery. Missing flags mean unavailable
sources; explicit unreadable files fail. Invalid arguments are sanitized. `--out`
uses exclusive creation with mode 0600 and never overwrites an existing file;
serialization completes before creation. Disk errors may leave a partial new
output. No automatic deletion occurs. Stdout is the default output.

`demo --format=text|json` performs actual local HTTP reads and independently
asserts duplicate, conflicting, partial, orphan, cutoff and 500-failure cases,
then closes clients/fixtures before rendering. Valid reports exit 0 regardless of
discrepancies; validation/I/O/assertion failures exit 1. There are no release,
remote deployment, inventory mutation or automatic repair operations.
