# PSB Roadmap

## Done

- [x] Go ingestion server (Echo v4, GORM, SQLite/PostgreSQL)
- [x] Data model: Environment (host_hash, cpu_model, cpu_flags, os, kernel_version, kernel_string, sm_version, deploy_mode) + ExecutionMetric
- [x] Batch JSONL ingestion (`POST /v1/telemetry`) with per-line validation
- [x] Triple gate check (X-PSB-Token, session cookie, X-PSB-Nonce)
- [x] Idempotent duplicate handling by (session_id, record_id)
- [x] Environment deduplication by SHA-256 hash
- [x] Path sanitization rejection (server-side)
- [x] Dashboard at root (`/`, `/session/:id`, `/record/:id`, `/env/:id`)
- [x] JSONL and JSON download endpoints
- [x] Viper config (flags + env vars)
- [x] Let's Encrypt autocert (--tls-domain)
- [x] Rate limiting middleware (--no-rate-limit for dev)
- [x] Protocol spec (docs/spec.md)
- [x] Fixture script for smoke testing

## MVP (next)

Priority order. Goal: end-to-end demo with real Snakemake workflows.

1. **Python client module** -- `PSBClient` with `add_record()` / `flush()` as specified in the spec. Ship as a standalone pip package (`snakemake-psb` or similar). This is the critical path.
2. **Snakemake integration point** -- hook into `benchmark:` directive completion. Call `add_record()` per rule, `flush()` on workflow exit via `atexit`. Must be opt-in (`--benchmark-telemetry`).
3. **Staging deployment** -- deploy to a public host with TLS, PostgreSQL, and a real domain. Validate the full pipeline end-to-end.
4. **Input size collection** -- the client needs to sum input file sizes and detect compound extensions (`.fastq.gz`). Straightforward but needs testing across platforms.

## Post-MVP

5. **Per-user API keys** -- replace the shared token with issued keys. Registration flow TBD (GitHub OAuth or institutional IdP).
6. **Rate limiting per key** -- move from IP-based to key-based rate limiting.
7. **gzip request bodies** -- accept `Content-Encoding: gzip` for large payloads.
8. **Aggregation API** -- percentiles, distributions per tool/platform/deploy_mode. Powers a public comparison dashboard.
9. **Statistical outlier detection** -- flag suspicious submissions (data poisoning defense).
10. **Tool version tracking** -- capture tool versions (e.g., `samtools 1.21`) for version-over-version regression analysis.

## Out of scope (for now)

- Workflow-level aggregation (DAG hash)
- Signed submissions (Ed25519)
- Streaming submission during workflow execution
- Multi-tenant / organization-level dashboards
