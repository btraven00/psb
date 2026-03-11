# PSB Telemetry Protocol Specification

**Version:** 0.1.0-draft
**Status:** MVP / Experimental
**Date:** 2026-03-11
**Authors**: btraven, ...

## 1. Introduction

The Planetary Scale Benchmark (PSB) is a crowd-sourced benchmarking system for tools commonly used in scientific workflows. Clients running Snakemake workflows voluntarily submit performance telemetry to a central ingestion service. The aggregated data enables cross-environment comparison of tool performance at global scale.

This document specifies the wire protocol, data model, ingestion API, and client responsibilities for the MVP implementation.

## 2. Terminology

| Term | Definition |
|------|-----------|
| **Session** | A single Snakemake workflow invocation (one `snakemake` run). Identified by `session_id`. |
| **Record** | Performance metrics for a single rule execution within a session. |
| **Environment** | A fingerprint of the execution platform (hardware, OS, Snakemake version). |
| **Tool** | The primary bioinformatics tool invoked by a rule (e.g., `samtools`, `bwa`). |

## 3. Data Model

### 3.1 Session

A session represents one Snakemake invocation. The `session_id` is a client-generated UUIDv4, created at workflow start and reused for all records in that run.

```
session_id: string (UUIDv4, e.g. "a3f1c9e0-7b2d-4e8a-b5c1-9d0e3f2a1b4c")
```

### 3.2 Record

A record captures metrics for a single rule execution. Each record is submitted as one JSON line.

**Record ID derivation:**

```
record_id = SHA256(rule_name + ":" + ISO8601_timestamp)[:16]
```

The record ID is a hex-encoded truncated hash, deterministic and collision-resistant within a session.

**Required fields (bare minimum for valid ingestion):**

| Field | JSON key | Type | Description |
|-------|----------|------|-------------|
| Session ID | `session_id` | string | UUIDv4 of the current Snakemake run |
| Record ID | `record_id` | string | Hash of `rule_name:timestamp` |
| Tool | `tool` | string | Tool label (see Section 3.4) |
| Runtime | `runtime_sec` | float | Wall-clock seconds, must be > 0 |
| Max RSS | `max_rss_mb` | float | Peak resident set size in MiB |
| CPU usage | `cpu_percent` | float | Average CPU utilization (e.g. 350.0 = 3.5 cores) |

**Optional fields:**

| Field | JSON key | Type | Description |
|-------|----------|------|-------------|
| Command | `command` | string | The shell command (e.g. `samtools sort`) |
| Parameters | `params` | string | Sanitized arguments (see Section 5.1) |
| Input size | `input_size` | int64 | Total input file size in bytes |
| Input type | `input_type` | string | File extension of the primary input (e.g. `.bam`, `.fastq.gz`) |
| Exit code | `exit_code` | int | Process exit code (0 = success) |

**Nonce (required by gate check, see Section 6):**

The `X-PSB-Nonce` header carries a random string generated per-request (not per-record). It is not included in the JSONL record body.

### 3.3 Environment

The environment is a manifest of the execution platform, submitted inline with each record. The server deduplicates environments by a deterministic hash.

| Field | JSON key | Type | Description |
|-------|----------|------|-------------|
| Host hash | `host_hash` | string | SHA256 hex digest of the hostname |
| CPU model | `cpu_model` | string | CPU model string (e.g. `Intel Xeon E5-2680 v4`, `Apple M2 Pro`) |
| CPU flags | `cpu_flags` | string | Comma-separated array of CPU flags (e.g. `sse4_2,avx,avx2,avx512f`) |
| Platform | `os` | string | One of: `linux`, `darwin`, `freebsd`, `windows` |
| Kernel version | `kernel_version` | string | Numeric kernel version (e.g. `6.1.0`) |
| Kernel string | `kernel_string` | string | Full kernel version string (e.g. `Linux 6.1.0-25-amd64`) |
| Snakemake version | `sm_version` | string | Snakemake version (e.g. `8.25.5`) |
| Deployment mode | `deploy_mode` | string | One of: `conda`, `docker`, `apptainer`, `envmodules`, `host` |

**Environment hash derivation:**

```
hash = SHA256("host_hash={host_hash}\ncpu_model={cpu_model}\ncpu_flags={cpu_flags}\nos={os}\nkernel={kernel_version}\nkernel_string={kernel_string}\nsm={sm_version}\ndeploy={deploy_mode}")
```

The server uses this hash as a unique index. Environments with the same hash are stored once.

### 3.4 Tool Inference

The `tool` field is a human-readable label identifying the bioinformatics tool.

- If the workflow provides an explicit tool label, use it.
- If not provided, derive `tool` from `command` (the first token of the shell command).
- **Drop rule:** If the derived command is a generic interpreter (`python`, `python3`, `bash`, `sh`, `Rscript`, `perl`, `java`, `ruby`), the record MUST be dropped by the client and not submitted. These are not meaningful benchmarks without further tool identification.

### 3.5 Input Type

The `input_type` is the file extension of the primary input file, including compound extensions:

- `.bam`, `.fastq.gz`, `.vcf.gz`, `.bed`, `.fasta`

This enables aggregation by data format.

## 4. Wire Format

### 4.1 Submission Format

The client submits records as **JSON Lines** (one JSON object per line, `\n`-delimited). Each line is a complete record with environment fields inlined.

```jsonl
{"session_id":"a3f1...","record_id":"b7e2...","tool":"samtools","command":"samtools sort","params":"-@ 4 -m 2G {input}","input_size":1073741824,"input_type":".bam","runtime_sec":42.3,"max_rss_mb":1024.5,"cpu_percent":380.2,"exit_code":0,"host_hash":"e3b0c4...","cpu_flags":"sse4_2,avx,avx2","os":"linux","kernel_version":"6.1.0","sm_version":"8.25.5","deploy_mode":"conda","x_psb_nonce":"f9a1..."}
{"session_id":"a3f1...","record_id":"c8d3...","tool":"bwa","command":"bwa mem","params":"-t 8 {input}","input_size":5368709120,"input_type":".fastq.gz","runtime_sec":312.7,"max_rss_mb":6200.0,"cpu_percent":790.1,"exit_code":0,"host_hash":"e3b0c4...","cpu_flags":"sse4_2,avx,avx2","os":"linux","kernel_version":"6.1.0","sm_version":"8.25.5","deploy_mode":"conda","x_psb_nonce":"a2b3..."}
```

### 4.2 Transport

The client accumulates records locally during the workflow run (one JSONL line per completed rule). At session end, the entire JSONL payload is submitted as a single HTTP POST to the ingestion endpoint.

```
POST /v1/telemetry
Content-Type: text/jsonl
X-PSB-Token: <token>
Cookie: _psb_session=<value>

{"session_id":"a3f1...","record_id":"b7e2...",...}\n
{"session_id":"a3f1...","record_id":"c8d3...",...}\n
```

The server splits the request body on `\n`, validates each line independently, and returns a summary response. This avoids per-record HTTP overhead (connection setup, TLS handshake, gate checks) and makes retry trivial -- resend the whole payload; duplicates are idempotent.

**Size limit:** The server SHOULD accept payloads up to 10 MiB uncompressed. A typical session with 500 records produces roughly 500 KiB of JSONL.

## 5. Client Responsibilities

### 5.1 Parameter Sanitization

Before submission, the client MUST sanitize the `params` field:

1. Replace all input file paths with `{input}`
2. Replace all output file paths with `{output}`
3. Replace log file paths with `{log}`
4. Replace temporary directory paths with `{tmpdir}`
5. Strip or mask any values for flags containing `email`, `key`, `token`, `password`, `secret`
6. The server rejects parameters containing absolute path patterns (`/home/`, `/Users/`, `C:\`, `/tmp/`)

### 5.2 Opt-in Consent

Telemetry submission MUST be opt-in. The Snakemake client MUST NOT send telemetry unless the user has explicitly enabled it (e.g., via `--benchmark-telemetry` flag or configuration).

### 5.3 Hostname Anonymization

The client MUST NOT submit the raw hostname. Instead, `host_hash` carries a SHA256 hash of the hostname. This allows correlation of records from the same machine without revealing identity.

## 6. Ingestion API

### 6.1 Endpoint

```
POST /v1/telemetry
Content-Type: text/jsonl
```

### 6.2 Authentication (MVP)

The MVP uses a best-effort triple gate check. This is NOT cryptographically secure authentication; it is a lightweight abuse deterrent. Production deployments SHOULD implement proper API key or OAuth-based authentication.

**Gate 1 -- Shared token (header):**

```
X-PSB-Token: <server-configured shared secret>
```

The token is distributed with the Snakemake client plugin. It is NOT a secret in the cryptographic sense; it is a speed bump against casual misuse.

**Gate 2 -- Session cookie:**

The client must include a `_psb_session` cookie with a non-empty value. This is set by the client on first contact and reused for the session lifetime.

**Gate 3 -- Nonce header:**

```
X-PSB-Nonce: <random string>
```

A random string generated per-request. This is a lightweight anti-replay measure.

**Failure mode:** If any gate fails, the server returns `418 I'm a teapot` with no further information. This deliberately provides no signal to automated abuse.

### 6.3 Request Validation

After gate checks pass, the server splits the request body on `\n` and validates each line independently:

1. Line is valid JSON
2. `session_id` is non-empty
3. `record_id` is non-empty
4. `tool` is non-empty
5. `runtime_sec` > 0
6. `params` does not contain absolute path patterns

Lines that fail validation are skipped and reported in the response. The request as a whole does not fail due to individual bad records.

### 6.4 Duplicate Handling

If a record with the same `(session_id, record_id)` pair already exists, it is counted as a duplicate. The original record is preserved unchanged. This makes submission idempotent -- the client can safely resend the entire JSONL payload on transient failure.

### 6.5 Response

**Success (201 Created):**

```json
{
  "status": "ok",
  "accepted": 47,
  "duplicates": 3,
  "rejected": 0,
  "errors": []
}
```

**Partial success (201 Created):**

```json
{
  "status": "ok",
  "accepted": 45,
  "duplicates": 3,
  "rejected": 2,
  "errors": [
    {"line": 12, "error": "runtime_sec must be > 0"},
    {"line": 38, "error": "tool is required"}
  ]
}
```

**Gate failure (418):**

```json
{
  "error": "I'm a teapot"
}
```

### 6.6 Data Retrieval API

The following read endpoints are available:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Paginated dashboard of all records |
| GET | `/session/:id` | Records for a specific session |
| GET | `/session/:id/jsonl` | Download session as JSON Lines |
| GET | `/env/:id` | Environment detail |
| GET | `/record/:id` | Single record detail |
| GET | `/record/:id/json` | Download record as JSON |

## 7. Python Client Module

The Snakemake telemetry plugin SHOULD implement the following interface:

```python
class PSBClient:
    """Client for submitting benchmark telemetry to the PSB service."""

    def __init__(self, endpoint: str, token: str):
        """
        Args:
            endpoint: Base URL of the PSB ingestion service.
            token: Shared X-PSB-Token value.
        """

    def start_session(self) -> str:
        """Generate and store a new UUIDv4 session ID. Returns the session_id."""

    def make_record_id(self, rule_name: str, timestamp: str) -> str:
        """Derive record_id as SHA256(rule_name:timestamp)[:16]."""

    def collect_environment(self) -> dict:
        """Gather environment manifest:
        - host_hash: SHA256(hostname)
        - cpu_model: CPU model string
        - cpu_flags: comma-separated CPU flags
        - os: standardized platform (linux/darwin/freebsd/windows)
        - kernel_version: numeric kernel version
        - kernel_string: full kernel version string
        - sm_version: snakemake.__version__
        - deploy_mode: one of conda/docker/apptainer/envmodules/host
        """

    def add_record(self, record: dict) -> None:
        """Append a record to the local buffer.
        
        Called after each rule completes with the benchmark: directive.
        The record dict must contain at minimum: record_id, tool, runtime_sec,
        max_rss_mb, cpu_percent. Environment fields are merged automatically.
        """

    def flush(self) -> dict:
        """POST all buffered records as a single JSONL payload to /v1/telemetry.
        
        Sets X-PSB-Token and X-PSB-Nonce headers, _psb_session cookie.
        Clears the buffer on success.
        
        Returns the server response dict with accepted/duplicates/rejected counts.
        On network failure, the buffer is preserved for retry.
        """
```

**Integration point:** During the workflow, each completed rule with a `benchmark:` directive calls `add_record()` with the extracted metrics. At workflow end (or on `atexit`), `flush()` submits the accumulated payload in a single HTTP request. Telemetry MUST NOT block workflow execution -- `flush()` should be fire-and-forget with a short timeout (e.g., 10s).

## 8. Security Considerations

### 8.1 Threat Model (MVP)

The MVP accepts that the ingestion endpoint is semi-public. The threat model covers:

| Threat | Mitigation (MVP) | Mitigation (Production) |
|--------|-------------------|------------------------|
| Spam / garbage data | Triple gate check (token + cookie + nonce) | API keys with rate limiting |
| Path leakage in params | Server-side rejection of absolute paths | Client-side stripping + server validation |
| Hostname fingerprinting | SHA256 hash of hostname | Same |
| Replay attacks | Duplicate detection by (session_id, record_id) | Timestamped nonces with expiry |
| Data poisoning (fake metrics) | None (trusted community model) | Statistical outlier detection |
| DDoS | None | Rate limiting, CDN, IP reputation |

### 8.2 Privacy

- No personally identifiable information (PII) is collected by design.
- Hostnames are hashed before submission.
- File paths are sanitized to placeholders.
- IP addresses are NOT stored by the application (though they may appear in access logs).

### 8.3 Future Authentication

Production deployments SHOULD replace the shared token with:

1. Per-user API keys issued via a registration flow
2. OAuth2 integration with institutional identity providers
3. Rate limiting per API key (e.g., 1000 records/hour)

## 9. Versioning

The API is versioned via URL path prefix (`/v1/`). Breaking changes to the wire format or validation rules require a new version (`/v2/`).

The `sm_version` field in the environment allows the server to apply version-specific parsing if the Snakemake benchmark output format changes.

## 10. Future Work (Out of Scope for MVP)

- Compression (gzip request bodies)
- Streaming submission (submit records during workflow execution, not just at end)
- Result aggregation API (percentiles, distributions per tool)
- Public leaderboard / comparison dashboard
- Signed submissions (client-side Ed25519 signatures)
- Tool version tracking (e.g., `samtools 1.21`)
- Workflow-level aggregation (DAG hash)
