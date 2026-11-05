# Contributing

Author and maintainer organization: [Lofrano Tecnologia](https://lofranotecnologia.com.br).
This is development version 0.1.0, licensed under MIT.

## Local development

Use an already installed Go 1.27 toolchain on Linux/POSIX. No external modules or
runtime services are required. Prepare the compiler separately; test/demo commands
must not acquire toolchains or use remote services.

```sh
mkdir -p build/cache build/tmp build/gopath
export GOTOOLCHAIN=local GOENV=off GOPROXY=off
export GOCACHE="$PWD/build/cache" GOTMPDIR="$PWD/build/tmp" GOPATH="$PWD/build/gopath"
go vet ./...
go test ./...
go test -race -timeout=90s ./...
go build -o build/fulfillment-reconcile ./cmd/fulfillment-reconcile
go run ./cmd/fulfillment-reconcile demo
go run ./cmd/fulfillment-reconcile demo --format=json
```

Format changed Go files with `gofmt`. Do not add generated binaries, lockfiles,
private data, credentials, telemetry, deployment or publication workflows.

## Review expectations

- Keep the standard-library-only dependency surface and pure reconciliation API.
- Read [docs/contract.md](docs/contract.md) before changing semantics. Update both
  READMEs, fixtures, contract and changelog when observable behavior changes.
- Keep every input and output path bounded, including typed API calls. Do not
  expose raw file, JSON, URL or response data in diagnostics. Successful report IDs
  are intentionally visible and must remain synthetic.
- Add hand-derived regression oracles, not tests that recalculate the implementation.
  Cover conflicting IDs moved between multiple pairs, missing sources/records,
  duplicate decoded keys, integer spelling/range, overflow and special files.
- Compare complete serialized reports for permutation/repetition invariance;
  never delete time or metadata fields to make a determinism test pass.
- Exercise actual loopback HTTP and assert request counts. No DNS, external
  interfaces, proxies or redirect destinations may be contacted by the client.
- Preserve caller data and detached results. Do not relax tests, truncate evidence,
  select first/last conflicting variants or silently treat invalid input as missing.
- Do not claim physical inventory completeness, actual lost-goods detection,
  standards certification, users, performance results or production readiness.

Use public repository issues for nonsensitive bugs with a minimal synthetic case.
Use [SECURITY.md](SECURITY.md) for private vulnerability reporting. General contact
is through the official homepage; do not add invented contact information.
