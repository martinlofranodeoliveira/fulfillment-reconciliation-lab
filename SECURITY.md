# Security policy

## Private reports

Please report vulnerabilities through GitHub private vulnerability reporting when available; otherwise request a private route through the official homepage:

https://github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/security/advisories/new

Do not publish sensitive details in a public issue. Include only synthetic
reproductions. General contact: https://lofranotecnologia.com.br

No response-time SLA or production support commitment is offered by this policy.
Version 0.1.0 is an unreleased development version.

## Scope

This is a local synthetic fulfillment reconciliation lab, not an inventory service.
It has no credentials, live provider integration, telemetry, financial functions or
runtime AI/API dependency. Do not supply real personal or business data. Successful
reports intentionally retain supplied business IDs; escaping is not anonymization.
Treat saved reports as visible local data. New CLI report files use mode 0600,
subject to the host filesystem; stdout protection is the caller's responsibility.

The parser enforces byte/depth/record/ID bounds, exact required properties,
decoded duplicate-key rejection and canonical integer/time rules. The typed API
also validates inputs. Overflow and excessive findings return no partial report.
The CLI accepts explicit bounded regular nonsymlink files on Linux/POSIX and
refuses to overwrite any existing output. A disk error may leave a partial newly
created report. Stable input files are the caller's responsibility.

HTTP reading is restricted to literal `127.0.0.1` fixture URLs with explicit ports
and the `/snapshot` path. DNS, credentials, queries, fragments, proxies, redirects
and decompression are disabled; headers, bodies, connection counts and deadlines
are bounded. Fixtures bind loopback only and own their payload bytes. The client
must be closed after use. Loopback is not authentication: another local process
may observe local activity or connect to a fixture while it is running. Do not
expose these fixtures through tunnels or port forwarding.

Diagnostics use controlled messages rather than raw paths, keys, values, endpoints,
network errors or response bodies. Resource bounds are not a claim of general
sandboxing, certification or production hardening. See [docs/contract.md](docs/contract.md)
for the exact supported subset and [docs/design.md](docs/design.md) for boundaries.
