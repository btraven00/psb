# PSB Telemetry Protocol Specification

| | |
|---|---|
| **Version** | 0.1.0-draft |
| **Status** | MVP / Experimental |
| **Date** | 2026-03-12 |
| **Authors** | btraven, ... |

## 1. Introduction

The Planetary Scale Benchmark (PSB) is a crowd-sourced benchmarking system for tools commonly used in scientific workflows. Clients running Snakemake workflows voluntarily submit performance telemetry to a central ingestion service. The aggregated data enables cross-environment comparison of tool performance at global scale.

This document specifies the wire protocol, data model, ingestion API, and client responsibilities for the MVP implementation.

## 2. Terminology

| Term | Definition |
|------|-----------|
| **Session** | A single Snakemake workflow invocation (one `snakemake` run). Identified by `session_id`. |
| **Record** | Performance metrics for a single rule execution within a session. |
| **Environment** | A fingerprint of the execution platform (hardware, OS, Snakemake version). |
| **Tool** | The primary scientific tool invoked by a rule (e.g., `samtools`, `bwa`, `minimap2`, `STAR`). |

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
record_id = SHA256(session_start_timestamp + ":" + rule_name + ":" + wildcards_str)[:16]
```

where `wildcards_str` is the sorted, comma-separated `key=value` pairs of the job's wildcards (empty string if no wildcards). For example, a rule `align` with wildcards `{sample: "A", lane: "L1"}` produces `wildcards_str = "lane=L1,sample=A"`.

The record ID is a hex-encoded truncated hash, deterministic within a session. Using the session start timestamp (rather than the current time) ensures that re-running the same rule with the same wildcards produces the same record ID, making submissions idempotent.

**Required fields (bare minimum for valid ingestion):**

| Field | JSON key | Type | Description |
|-------|----------|------|-------------|
| Session ID | `session_id` | string | UUIDv4 of the current Snakemake run |
| Record ID | `record_id` | string | Hash of `session_start:rule_name` |
| Tool | `tool` | string | Tool label (see Section 3.4) |
| Runtime | `runtime_sec` | float | Wall-clock seconds, must be > 0 |
| Max RSS | `max_rss_mb` | float | Peak resident set size in MiB |
| CPU usage | `cpu_percent` | float | Average CPU utilization (e.g. 350.0 = 3.5 cores) |

**Optional fields:**

| Field | JSON key | Type | Description |
|-------|----------|------|-------------|
| Command | `command` | string | The shell command (e.g. `samtools sort`) |
| Parameters | `params` | string | Sanitized arguments (see Section 5.1) |
| Input size | `input_size` | int64 | Total size of all input files in bytes |
| Num inputs | `num_inputs` | int | Number of input files |
| Input type | `input_type` | string | Comma-separated unique file extensions of inputs (e.g. `.fastq.gz,.fa,.bed`) |
| Output size | `output_size` | int64 | Total size of all output files in bytes |
| Threads | `threads` | int | Number of threads allocated to this job by Snakemake |
| Exit code | `exit_code` | int | Process exit code (0 = success) |
| Load average | `load_avg` | float | 1-minute load average at job start |
| Memory available | `mem_avail_mb` | int | Available memory in MB at job start |
| Swap used | `swap_used_mb` | int | Swap in use in MB at job start |
| I/O wait | `io_wait_pct` | float | I/O wait percentage during the job (Linux only, 0 on other platforms) |

**Nonce (required by gate check, see Section 6):**

The `X-PSB-Nonce` header carries a random string generated per-request (not per-record). It is not included in the JSONL record body.

### 3.3 Environment

The environment is a manifest of the execution platform, submitted inline with each record. The server deduplicates environments by a deterministic hash.

| Field | JSON key | Type | Description |
|-------|----------|------|-------------|
| Host hash | `host_hash` | string | SHA256 hex digest of the hostname |
| CPU model | `cpu_model` | string | CPU model string (e.g. `Intel Xeon E5-2680 v4`, `Apple M2 Pro`) |
| CPU features | `cpu_features` | uint64 | Bitmask of curated CPU feature flags (see Section 3.6) |
| CPU cores | `cpu_cores` | int | Physical CPU core count |
| L2 cache | `l2_cache_kb` | int | L2 cache size in KB |
| L3 cache | `l3_cache_kb` | int | L3 cache size in KB |
| CPU frequency | `cpu_freq_mhz` | int | Max CPU frequency in MHz |
| Platform | `os` | string | One of: `linux`, `darwin`, `freebsd`, `windows` |
| Kernel version | `kernel_version` | string | Numeric kernel version (e.g. `6.1.0`) |
| Kernel string | `kernel_string` | string | Full kernel version string (e.g. `Linux 6.1.0-25-amd64`) |
| Snakemake version | `sm_version` | string | Snakemake version (e.g. `8.25.5`) |
| Deployment mode | `deploy_mode` | string | Deployment method(s) in use (see Section 3.7) |

**Deprecated fields (backward compatibility):**

| Field | JSON key | Type | Description |
|-------|----------|------|-------------|
| CPU flags | `cpu_flags` | string | Raw comma-separated CPU flags. Replaced by `cpu_features` bitmask. Old clients may still send this; the server auto-encodes it to the bitmask. |

**Environment hash derivation:**

```
hash = SHA256("host_hash={host_hash}\ncpu_model={cpu_model}\ncpu_flags={cpu_flags}\ncpu_features={cpu_features}\ncpu_cores={cpu_cores}\nl2={l2_cache_kb}\nl3={l3_cache_kb}\nfreq={cpu_freq_mhz}\nos={os}\nkernel={kernel_version}\nkernel_string={kernel_string}\nsm={sm_version}\ndeploy={deploy_mode}")
```

The server uses this hash as a unique index. Environments with the same hash are stored once.

### 3.4 Tool Inference

The `tool` field is a human-readable label identifying the scientific tool.

- If the workflow provides an explicit tool label, use it.
- If not provided, derive `tool` from `command` (the first token of the shell command).
- **Drop rule:** If the derived command is a generic interpreter (`python`, `python3`, `bash`, `sh`, `Rscript`, `perl`, `java`, `ruby`), the record MUST be dropped by the client and not submitted. These are not meaningful benchmarks without further tool identification.

### 3.5 Input Type

The `input_type` field captures the file extensions of input files as a comma-separated string of unique extensions, preserving compound extensions:

- Single input: `.bam`
- Multiple inputs of same type: `.fastq.gz`
- Mixed inputs: `.fastq.gz,.fa,.bed`

This enables aggregation by data format.

### 3.6 CPU Features Bitmask

The `cpu_features` field encodes curated CPU feature flags relevant to scientific computing workloads into a compact `uint64` bitmask. This replaces the verbose raw `cpu_flags` string.

**Bit assignments (must be identical in client and server):**

| Bit | Feature | Category |
|-----|---------|----------|
| 0 | sse2 | x86 SIMD |
| 1 | sse3 | x86 SIMD |
| 2 | ssse3 | x86 SIMD |
| 3 | sse4_1 | x86 SIMD |
| 4 | sse4_2 | x86 SIMD |
| 5 | avx | x86 SIMD |
| 6 | avx2 | x86 SIMD |
| 7 | fma | x86 SIMD |
| 8 | avx512f | x86 SIMD |
| 9 | avx512bw | x86 SIMD |
| 10 | avx512vl | x86 SIMD |
| 11 | avx512dq | x86 SIMD |
| 12 | avx512_vnni | x86 SIMD (ML inference) |
| 13 | f16c | x86 half-precision float |
| 14 | popcnt | Bit manipulation |
| 15 | bmi1 | Bit manipulation |
| 16 | bmi2 | Bit manipulation |
| 17 | abm (lzcnt) | Bit manipulation |
| 18 | aes | Crypto |
| 19 | sha | Crypto |
| 20 | pclmulqdq | Crypto (checksums) |
| 21 | rdrand | Hardware RNG |
| 22 | tsx | Transactional memory |
| 23 | neon (asimd) | ARM SIMD |
| 24 | sve | ARM scalable vectors |
| 25 | sve2 | ARM scalable vectors |
| 26 | crc32 | CRC |
| 27 | xop | AMD |

**Encoding:** The client reads raw CPU flag names from the OS (e.g. `/proc/cpuinfo` on Linux, `sysctl` on macOS) and ORs the corresponding bits. Multiple OS flag names may map to the same bit (e.g. `pni` and `sse3` both map to bit 1, `asimd` and `neon` both map to bit 23).

**Backward compatibility:** If a client sends `cpu_flags` (raw string) but no `cpu_features`, the server auto-encodes the string to the bitmask. For display and export, the server decodes the bitmask back to canonical flag names.

### 3.7 Deployment Mode

The `deploy_mode` field reflects the Snakemake `--software-deployment-method` setting:

- `host` — no software deployment method (bare metal)
- `conda` — Conda environments
- `apptainer` — Apptainer/Singularity containers
- `env_modules` — Environment modules

When multiple methods are active, they are joined with `+` in sorted order (e.g. `conda+apptainer`).

### 3.8 System State (Distress Metrics)

Per-rule system state metrics capture the health of the execution environment at the time the job ran. These are optional fields on the record, captured at job start (load, memory, swap) and as a delta over the job duration (iowait).

| Field | JSON key | Type | Capture point | Description |
|-------|----------|------|---------------|-------------|
| Load average | `load_avg` | float | Job start | 1-minute load average. Values >> cpu_cores indicate CPU contention. |
| Memory available | `mem_avail_mb` | int | Job start | Available memory in MB. Low values indicate memory pressure. |
| Swap used | `swap_used_mb` | int | Job start | Swap in use in MB. Any non-zero value during scientific computing is a red flag. |
| I/O wait | `io_wait_pct` | float | Delta over job | Percentage of CPU time spent waiting for I/O during the job. High values indicate disk contention. Linux only (0 on other platforms). |

**Purpose:** These metrics allow downstream analysis to flag unreliable runtime measurements. A job that ran while the system was swapping, overloaded, or I/O-bound may have inflated runtimes that should be excluded from benchmarks or weighted accordingly.

**Platform support:**

| Metric | Linux | macOS |
|--------|-------|-------|
| load_avg | `os.getloadavg()` | `os.getloadavg()` |
| mem_avail_mb | `/proc/meminfo` MemAvailable | `vm_stat` (free + inactive pages) |
| swap_used_mb | `/proc/meminfo` SwapTotal - SwapFree | `sysctl vm.swapusage` |
| io_wait_pct | `/proc/stat` iowait delta | Not available (returns 0) |

## 4. Wire Format

### 4.1 Submission Format

The client submits records as **JSON Lines** (one JSON object per line, `\n`-delimited). Each line is a complete record with environment fields inlined.

```jsonl
{"session_id":"a3f1...","record_id":"b7e2...","tool":"samtools","command":"samtools sort","params":"-@ 4 -m 2G {input}","input_size":1073741824,"num_inputs":1,"input_type":".bam","output_size":524288000,"runtime_sec":42.3,"threads":4,"max_rss_mb":1024.5,"cpu_percent":380.2,"exit_code":0,"load_avg":2.15,"mem_avail_mb":32000,"swap_used_mb":0,"io_wait_pct":1.2,"host_hash":"e3b0c4...","cpu_model":"Intel Xeon E5-2680 v4","cpu_features":16351,"cpu_cores":16,"l2_cache_kb":256,"l3_cache_kb":30720,"cpu_freq_mhz":3300,"os":"linux","kernel_version":"6.1.0","sm_version":"8.25.5","deploy_mode":"conda"}
```

### 4.2 Transport

The client accumulates records locally during the workflow run (one JSONL line per completed rule). At session end, the entire JSONL payload is submitted as a single HTTP POST to the ingestion endpoint.

```
POST /v1/telemetry
Content-Type: text/jsonl
X-PSB-Token: <token>
X-PSB-Nonce: <random string>
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

Telemetry submission MUST be opt-in. The Snakemake client MUST NOT send telemetry unless the user has explicitly enabled it (e.g., via `--share-benchmark` flag or configuration).

### 5.3 Hostname Anonymization

The client MUST NOT submit the raw hostname. Instead, `host_hash` carries a SHA256 hash of the hostname. This allows correlation of records from the same machine without revealing identity.

### 5.4 System State Capture

The client SHOULD capture system state metrics (load average, available memory, swap usage) immediately before each benchmarked job starts. For I/O wait, the client SHOULD sample `/proc/stat` before and after the job and compute the delta percentage. These metrics have near-zero overhead (single procfs reads).

### 5.5 Input/Output Measurement

- **Input size:** Sum the sizes of all input files (not just the first).
- **Num inputs:** Count the number of successfully stat'd input files.
- **Input type:** Collect unique file extensions across all inputs, comma-separated.
- **Output size:** Sum the sizes of all output files after job completion.

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
| GET | `/session/:id/parquet` | Download session as Apache Parquet |
| GET | `/env/:id` | Environment detail |
| GET | `/record/:id` | Single record detail |
| GET | `/record/:id/json` | Download record as JSON |

### 6.7 Export Formats

**JSONL export** (`/session/:id/jsonl`): Each line is a JSON object containing `{"metric": {...}, "environment": {...}}` with all fields.

**Parquet export** (`/session/:id/parquet`): A flat table with one row per record. Environment and metric fields are merged into a single row. CPU features are decoded from bitmask to comma-separated string for readability. All fields including distress metrics are included.

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

    session_start: str
    """ISO8601 timestamp recorded at client initialization.
    Used for deterministic record_id derivation."""

    def make_record_id(self, rule_name: str, wildcards: dict | None = None) -> str:
        """Derive record_id as SHA256(session_start:rule_name:wildcards_str)[:16].

        Uses the session start timestamp (not current time) and sorted
        wildcard key=value pairs to ensure unique, idempotent record IDs.
        """

    def collect_environment(self) -> dict:
        """Gather environment manifest:
        - host_hash: SHA256(hostname)
        - cpu_model: CPU model string
        - cpu_features: uint64 bitmask of curated CPU flags (see Section 3.6)
        - cpu_cores: physical core count
        - l2_cache_kb: L2 cache size in KB
        - l3_cache_kb: L3 cache size in KB
        - cpu_freq_mhz: max CPU frequency in MHz
        - os: standardized platform (linux/darwin/freebsd/windows)
        - kernel_version: numeric kernel version
        - kernel_string: full kernel version string
        - sm_version: snakemake.__version__
        - deploy_mode: deployment method(s) (see Section 3.7)
        """

    def add_record(self, record: dict) -> None:
        """Append a record to the local buffer.

        Called after each rule completes with the benchmark: directive.
        The record dict must contain at minimum: record_id, tool, runtime_sec,
        max_rss_mb, cpu_percent. Environment fields are merged automatically.

        Optional fields: command, params, input_size, num_inputs, input_type,
        output_size, threads, exit_code, load_avg, mem_avail_mb, swap_used_mb,
        io_wait_pct.
        """

    def flush(self) -> dict:
        """POST all buffered records as a single JSONL payload to /v1/telemetry.

        Sets X-PSB-Token and X-PSB-Nonce headers, _psb_session cookie.
        Clears the buffer on success.

        Returns the server response dict with accepted/duplicates/rejected counts.
        On network failure, the buffer is preserved for retry.
        """
```

**System state capture helpers:**

```python
def capture_system_snapshot() -> dict:
    """Capture system state at a point in time.

    Returns: {
        "load_avg": float,      # 1-min load average
        "mem_avail_mb": int,    # available memory in MB
        "swap_used_mb": int,    # swap in use in MB
        "iowait_start": (int, int),  # raw (iowait_ticks, total_ticks) for delta
    }
    """

def compute_iowait_pct(start: tuple, end: tuple) -> float:
    """Compute iowait % from before/after tick snapshots."""
```

**Integration point:** During the workflow, each completed rule with a `benchmark:` directive calls `add_record()` with the extracted metrics. System state is captured before each benchmarked job starts and the iowait delta is computed after completion. At workflow end (or on `atexit`), `flush()` submits the accumulated payload in a single HTTP request. Telemetry MUST NOT block workflow execution -- `flush()` should be fire-and-forget with a short timeout (e.g., 10s).

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
| DDoS | Rate limiting (configurable req/s per IP) | CDN, IP reputation |

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

## 10. Database Indexes

The server maintains the following indexes for query performance:

| Table | Index | Type |
|-------|-------|------|
| `environments` | `hash` | Unique |
| `execution_metrics` | `(session_id, record_id)` | Unique composite |
| `execution_metrics` | `tool` | Non-unique |

## 11. Future Work (Out of Scope for MVP)

- Compression (gzip request bodies)
- Streaming submission (submit records during workflow execution, not just at end)
- Result aggregation API (percentiles, distributions per tool)
- Public leaderboard / comparison dashboard
- Signed submissions (client-side Ed25519 signatures)
- Tool version tracking (e.g., `samtools 1.21`)
- Workflow-level aggregation (DAG hash)
- Rule name capture (Snakemake rule name alongside tool)
- Non-zero exit code recording (capture metrics even on failure)
- Periodic background flushes (every N records or M seconds)
