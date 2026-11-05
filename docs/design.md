# Design — development 0.1.0

Author: Lofrano Tecnologia — https://lofranotecnologia.com.br

## Scope

Three explicit synthetic exports enter a pure reconciliation function. The lab
explains supplied unit/evidence differences, not physical inventory. No real
customer data, money, credentials, provider adapter, telemetry or remote API is
needed. Development scope is intentionally smaller than a warehouse integration.

## Pipeline

```text
Explicit regular files ── bounded reader ── strict JSON ──┐
                                                       ├─ typed validation
Three loopback fixtures ── bounded HTTP ── strict JSON ─┘
                                                           │
                                      exact tuple union + event groups
                                                           │
                                         checked sums + classification
                                                           │
                                      sorted detached report → text/JSON
```

`pkg/model` defines versioned records, limits and typed validation.
`pkg/strictjson` first scans tokens with bounded depth and decoded object-key sets.
Only after that scan does it construct a value tree and check required exact-case
properties and integer token spelling/range against the small model types. A
final strict decoder constructs the typed snapshot. This extra bounded pass is
preferable to accepting `encoding/json`'s null-to-zero and case-insensitive struct
matching. Native decoder messages are deliberately not propagated.

Typed callers pass through the same semantic validation at the reconciliation
boundary, including count, field, canonical timestamp and serialized source-size
bounds. Record/field checks precede serialization so that the temporary allocation
has a fixed upper bound. JSON whitespace counts against the raw byte limit; typed
callers have no whitespace and are measured by compact encoding.

## Determinism and ownership

There is no ambient clock, random source, global mutable state or retained caller
slice in reconciliation. Keys are two-field structs rather than delimiter joins.
The engine builds its own bounded tuple union and reservation ID groups. Group
conflict detection compares decoded complete records, accumulates every referenced
tuple, and excludes every conflicting variant. The stored first record is used
only when all records in the group are identical. Conflicting totals and notes
therefore cannot depend on encounter order. Unconflicted totals use positive
checked addition, so any order either yields the same total or an overflow error.

Reports sort findings, evidence IDs and reason codes. Fresh containers isolate
returned reports from input and later results. No generation timestamp is included;
only explicit declared cutoff metadata is exported. Tests compare complete JSON,
without deleting or normalizing fields. Concurrent caller mutation during a call
is outside the contract.

## Evidence semantics

Global incompleteness takes precedence over all classifications, while preserving
conflict reasons. This prevents missing order sources from producing false orphan
conclusions and mismatched cutoffs from certifying matches. With aligned sources,
conflicts precede orphan detection. For requested tuples, absent reservation or
shipment records are incomplete rather than an asserted zero. Nonempty partial
shipments remain numeric inconsistencies. A zero unquarantined subtotal is not a
selected conflicting variant.

Shipment records are unique snapshot lines, not replayable events. Repeated
shipment IDs reject the source; only identical reservation event IDs deduplicate.
This distinction prevents accidental shipment replay inflation or silent loss of
invalid snapshot evidence. It is a deliberate correction to the earlier ambiguous
shipment duplicate behavior within this unreleased development version.

The finding cap fails before building a report rather than truncating rows and
misstating totals. All input bounds are small, explicit constants. Working storage
is linear in bounded payload bytes, records, evidence references and tuples; there
is no variant cross-product. Evidence references across groups are bounded by raw
reservation entries. Rendered CLI output has a separate byte cap.

## Local HTTP boundary

The client validates a very narrow literal URL subset, then its dialer independently
checks the address. Neither validation nor dialing resolves DNS. Proxy configuration
is nil, redirects return without following, compression is disabled, headers and
body bytes are bounded, and deadlines apply to reads. Native URL/network/response
values never become diagnostics. A controlled numeric HTTP status is safe to show.
Idle transport connections are explicitly closed.

Fixtures bind `tcp4` at `127.0.0.1:0`, copy response slices, cap stored payloads,
limit active connections and configure header/read/write/idle deadlines. Close
marks the fixture closed under the handler admission mutex, closes HTTP connections,
waits for Serve, then waits for admitted handlers. This prevents WaitGroup admission
races and leaves no serving goroutine behind. The extra payload detection byte is
an intentional adversarial testing facility, not an unbounded fixture.

The demo serializes synthetic typed examples into fixture response JSON and then
uses the actual HTTP decoding path. A subsequent 500 is actually fetched and mapped
to an unavailable source. Cutoff mismatch uses an explicit copied metadata change.
Expected classifications/totals are hand-written, not calculated by the engine.
Only after cleanup are deterministic text or JSON results emitted.

## Filesystem and diagnostic boundary

The CLI requires explicit files, checks regular-file type and size, opens
nonblocking with no-follow on Linux/POSIX and rechecks descriptor identity/type.
All errors use controlled messages. Output creation is exclusive, so an input or
existing report cannot be truncated through an alias. Input mutation by an external
writer during reading is not snapshot isolation: the bounded reader/parser still
validates the bytes obtained. Use stable synthetic exports. Filesystem write
failures can leave a partial newly created report and are reported, not hidden.

## Verification strategy

Tests include hand-derived split/partial quantities, 5/4 conflicts, three moved
references, identical replay, all-source failures, missing rows, full serialized
permutation invariance, ownership, delimiter collision, int64 overflow and exact
finding-limit behavior. Parser tests cover escaped duplicate keys, required/null
fields, exact casing, lexical integer forms, UTF-8, depth, bytes and committed
fixtures. Actual loopback tests count requests, reject redirects/proxies, reject
DNS/other interfaces before dialing, and exercise 500, streamed oversized bodies,
large headers and deadlines. CLI tests exercise FIFOs, symlinks, output aliasing,
failed writes and complete deterministic demo output. These are test definitions,
not a statement that they have been run in any environment.

## Standard-library references

- https://pkg.go.dev/encoding/json#Decoder.UseNumber — preserves numeric tokens;
  additional lexical, duplicate-key and schema checks remain necessary.
- https://pkg.go.dev/net/http — explicit transport/server resource configuration.
- https://pkg.go.dev/net/http/httptest — used with literal loopback listeners for
  adversarial HTTP tests, not evidence of production deployment.

No broad JSON, timestamp, logistics or security-standard compliance is claimed.
