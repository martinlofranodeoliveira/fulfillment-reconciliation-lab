# Changelog

## 0.1.0 — Unreleased

Development version; no release or deployment is claimed.

- Reject empty URL fragments before dialing and retain the default checksum-database policy for any future explicit dependency preparation.

### Added

- Original standard-library Go reconciliation API for three explicitly versioned
  synthetic order, reservation and shipment snapshots.
- Exact tuple correlation, checked positive int64 aggregation, replay deduplication,
  explainable conflict quarantine, missing-evidence and quantity classifications.
- Bounded strict JSON readers, explicit local-file CLI and actual loopback HTTP
  fixtures with deterministic text/JSON demonstrations.
- English/Portuguese instructions, precise contract, security policy and read-only
  test/build/demo CI targeting Go 1.27.

### Corrected during development

- Invalid parser tokens and missing demo import prevented compilation.
- Removed wall-clock report timestamps; complete reports now depend only on input.
- Enforced typed API validation, canonical UTC times, explicit schema version and
  raw record counts, including caller-supplied typed snapshots.
- Replaced delimiter-based order keys with tuples and first-variant conflict sums
  with exclusion of every conflicting variant and quarantine of all references.
- Made unavailable sources, differing cutoffs and absent records incomplete;
  preserved conflict reasons without falsely proving matches or orphan identities.
- Replaced misleading finding truncation with a whole-operation bounded error.
- Distinguished unique shipment snapshot IDs from replayable reservation event IDs.
- Hardened decoded duplicate-key, exact required-field, null and integer-token checks.
- Restricted HTTP validation/dialing to a literal loopback fixture subset, sanitized
  diagnostics and bounded headers, connections, payload ownership and cleanup.
- Protected CLI inputs and existing outputs from special-file blocking/overwrite.
- Added independent regression/adversarial oracles and complete serialized
  determinism/ownership tests. Test execution is not asserted by this changelog.
