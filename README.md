# Fulfillment Reconciliation Lab

**Development version 0.1.0 — unreleased.** Original MIT software by
[Lofrano Tecnologia](https://lofranotecnologia.com.br).
[Português](README.pt-BR.md) · [Contract](docs/contract.md) · [Design](docs/design.md)

Correlate synthetic requested order/SKU quantities, reservation events and shipment
snapshots. Explain duplicate deliveries, conflicting evidence, split shipments and
missing sources. There is no inventory write, money, payment, provider integration,
telemetry or runtime model/API dependency.

## Install locally

Use an already installed **Go 1.27** toolchain and an extracted source directory.
The CLI targets Linux/POSIX; Windows CLI support is not provided. The Go library
uses portable standard-library code. There are no external modules to download.
Toolchain preparation is separate from the offline commands below.

From the project directory:

```sh
mkdir -p build/cache build/tmp build/gopath
export GOTOOLCHAIN=local GOENV=off GOPROXY=off
export GOCACHE="$PWD/build/cache" GOTMPDIR="$PWD/build/tmp" GOPATH="$PWD/build/gopath"
go build -o build/fulfillment-reconcile ./cmd/fulfillment-reconcile
build/fulfillment-reconcile help
```

These settings keep Go caches/temp files local and prohibit automatic toolchain
acquisition and module resolution. No HOME workspace discovery is performed.

## Tests and deterministic offline demo

```sh
go test ./...
go test -race -timeout=90s ./...
go run ./cmd/fulfillment-reconcile demo
go run ./cmd/fulfillment-reconcile demo --format=json
```

The demo starts three real bounded HTTP fixtures on `127.0.0.1`, fetches all three
JSON snapshots through `net/http`, checks hand-derived totals/classifications,
then exercises a different cutoff and an actual HTTP 500 shipment source failure.
Clients and fixtures close before output is written. No ephemeral URL or ambient
clock appears in the output. Repeated complete JSON demo outputs are identical.

Beginning of the text demo output:

```text
scenario=duplicates_conflict_partial_orphan
Fulfillment Reconciliation Lab 0.1.0 (development)
cutoffs_aligned=true pairs=4 matched=1 inconsistent=1 ambiguous=1 incomplete=0 orphan=1
ORDER/SKU CLASSIFICATION requested reserved shipped remaining
o1/s1 MATCHED 5 5 5 0 [EXACT_MATCH]
o2/s2 AMBIGUOUS_EVIDENCE 5 0 5 0 [CONFLICTING_RESERVATION_EVENT]
  Conflicting reservation variants excluded; totals are unquarantined subtotals.
o3/s3 INCONSISTENT_QUANTITIES 5 5 3 2 [SHIPMENT_SHORTFALL,UNFULFILLED_RESERVATION]
o999/s999 ORPHAN_EVIDENCE 0 0 2 -2 [ORPHAN_EVIDENCE_UNRECOGNIZED_ORDER]
```

The remaining output includes four incomplete findings for the cutoff scenario,
three for the unavailable shipment source, and request counts
`orders=1 reservations=1 shipments=2`. Assertions fail the command on a discrepancy.

## File CLI

```sh
build/fulfillment-reconcile reconcile \
  --orders=testdata/fixtures/orders_aligned.json \
  --reservations=testdata/fixtures/reservations_aligned.json \
  --shipments=testdata/fixtures/shipments_aligned.json
```

Those fixtures yield three matched pairs; the first has shipped units `3+2=5` and
an identical reservation replay counted once. JSON export to a **new** file:

```sh
build/fulfillment-reconcile reconcile \
  --orders=testdata/fixtures/orders_aligned.json \
  --reservations=testdata/fixtures/reservations_aligned.json \
  --shipments=testdata/fixtures/shipments_aligned.json \
  --format=json --out=build/report.json
```

Existing output paths are refused rather than overwritten. Omit a source flag to
represent an unavailable source; an explicitly supplied unreadable or invalid file
is an error, not silently missing evidence. At least one file is required.
Inputs must be regular nonsymlink files; FIFOs/directories are rejected. Exit status
is zero for a valid report, including discrepancies, and one for an operational,
validation or demo assertion failure. Diagnostics exclude raw paths/payloads.

## Small Go usage example

```go
orders, err := strictjson.DecodeOrderSnapshot(reader)
if err != nil {
    return err
}
report, err := reconcile.Reconcile(orders, nil, nil)
if err != nil {
    return err
}
// report has incomplete findings because two sources are unavailable.
_ = report
```

Import `pkg/strictjson` and `pkg/reconcile` under
`github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab`.
The pure `Reconcile` API also validates typed snapshots. It does not mutate input;
returned containers are detached. Callers must not concurrently mutate inputs.

## Rationale and limits

- Exact, case-sensitive tuple IDs avoid fuzzy associations and delimiter collisions.
- Only identical reservation **event IDs** deduplicate. A conflicting ID excludes
  every variant from totals and quarantines every referenced pair. Shipment IDs
  and order/SKU tuples must be unique; duplicates are rejected.
- Missing sources or differing cutoffs take classification precedence as
  `INCOMPLETE_EVIDENCE`; conflict reasons remain visible. With aligned sources,
  absent reservation/shipment rows are also incomplete. A partial nonempty
  shipment totaling 3 against requested/reserved 5 is inconsistent, remaining 2.
- Limits: 2 MiB/source, 5,000 records/source, JSON container depth 8, ASCII IDs
  1–128 characters, positive int64 units, 10,000 findings. Overflow or too many
  findings fails the whole report; nothing is silently truncated. CLI rendering
  is capped at 16 MiB.
- HTTP reading is deliberately restricted to
  `http://127.0.0.1:<port>/snapshot`, without credentials, queries, fragments,
  compression, proxies, redirects or DNS. It is a fixture reader, not an adapter.
- Canonical UTC seconds only, explicit schema version `0.1.0`, exact required
  fields, no nulls, unknown fields or duplicate decoded JSON keys. This is a narrow
  schema, not a claim of general JSON/time-standard compliance.
- Supplied snapshots may omit reality. Equal declared cutoffs do not certify
  completeness, physical stock or actual lost goods. No repairs/replays are sent.
- Business IDs are intentionally visible in reports and are not anonymized.
  Use synthetic data only. A failed filesystem write can leave a partial newly
  created report; input and preexisting output files are not overwritten.

## Project

Repository: https://github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md) and
[CHANGELOG.md](CHANGELOG.md). General contact is through the official homepage.
[MIT License](LICENSE), copyright 2026 Lofrano Tecnologia.
