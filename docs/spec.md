# PSB Telemetry Protocol Specification

| | |
|---|---|
| **Version** | 0.1.0-draft |
| **Status** | experimental |
| **Date** | 2026-03-12 |
| **Authors** | btraven, ... |

## 1. Introduction

The Planetary Scale Benchmark (PSB) is a crowd-sourced benchmarking system for scientific workflow tools. Clients running Snakemake workflows voluntarily submit performance telemetry to a central ingestion service. The aggregated data enables cross-environment comparison of tool performance at global scale.

This document specifies the data model, submission protocol, and client responsibilities.

## 2. Terminology

| Term | Definition |
|------|-----------|
| **Session** | A single workflow invocation. Identified by `session_id` (UUIDv4). |
| **Observation** | Performance data for one rule execution within a session. |
| **Environment** | A fingerprint of the execution platform (hardware, OS, runtime version). |
| **Tool** | The primary scientific tool invoked by a rule (e.g. `samtools`, `bwa`, `STAR`). |
| **Command** | The detected or annotated command being benchmarked. May be a subcommand of the tool (e.g. `sort` for `samtools sort`), an entrypoint script (e.g. `scripts/normalize.py`), or — in pipelines — the specific stage the user designates via annotation. |

---

## Part I: Data Model

The data model defines three independent entities. None of these carry transport or authentication concerns.

### 3. Session

A session represents one workflow invocation.

| Field | Key | Type | Description |
|-------|-----|------|-------------|
| Session ID | `session_id` | string | Client-generated UUIDv4, created at workflow start |
| Workflow URL | `workflow_url` | string | Repository or release URL (scheme + host + path only, no query/fragment/auth). Optional. |
| Workflow version | `workflow_version` | string | Version tag (e.g. `v1.2.0`, `main@abc1234`). Optional. |

The client reads `workflow_url` and `workflow_version` from the workflow config `psb:` section (see Section 8).

### 4. Observation

An observation captures performance data for a single rule execution.

#### 4.1 Record ID

```
record_id = SHA256(session_start_iso + ":" + rule_name + ":" + wildcards_str)[:16]
```

`wildcards_str` is sorted, comma-separated `key=value` pairs (empty string if none). The session start timestamp makes the ID deterministic within a session and idempotent across resubmissions.

#### 4.2 Required Fields

| Field | Key | Type | Description |
|-------|-----|------|-------------|
| Record ID | `record_id` | string | Hex-encoded truncated hash (see above) |
| Tool | `tool` | string | Tool label (see Section 6) |
| Runtime | `runtime_sec` | float | Wall-clock seconds, must be > 0 |
| Max RSS | `max_rss_mb` | float | Peak resident set size in MiB |
| CPU usage | `cpu_percent` | float | Cumulative CPU-seconds (e.g. 380.0 for ~4 cores fully utilized) |

#### 4.3 Optional Fields

**Tool context:**

| Field | Key | Type | Description |
|-------|-----|------|-------------|
| Command | `command` | string | Shell command or subcommand (e.g. `samtools sort`) |
| Parameters | `params` | string | Sanitized arguments (see Section 10.1) |
| Shell block | `shell_block` | string | Full unresolved shell template from the rule (see Section 7) |
| Tool version | `tool_version` | string | Version of the primary tool (e.g. `1.21`) |
| Category | `category` | string | Workflow step category for cross-method aggregation (e.g. `alignment`) |

<!-- INPUT NEEDED: Should `category` be a single string or replaced with a `tags` list for more flexible classification? -->

**I/O:**

| Field | Key | Type | Description |
|-------|-----|------|-------------|
| Inputs | `inputs` | array\<FileEntry\> | Per-file `{"type": string, "size": int64}` (see Section 5) |
| Outputs | `outputs` | array\<FileEntry\> | Per-file `{"type": string, "size": int64}` |

**Resources:**

| Field | Key | Type | Description |
|-------|-----|------|-------------|
| Threads | `threads` | int | Threads allocated by Snakemake |
| Resources | `resources` | string | JSON-encoded dict of resource allocations (e.g. `{"_cores":4,"mem_mb":8000}`) |
| Exit code | `exit_code` | int | Process exit code (0 = success) |

**Extended memory/IO (from `psutil` process monitoring):**

| Field | Key | Type | Description |
|-------|-----|------|-------------|
| Max VMS | `max_vms_mb` | float | Peak virtual memory size in MiB |
| Max USS | `max_uss_mb` | float | Peak unique set size in MiB |
| Max PSS | `max_pss_mb` | float | Peak proportional set size in MiB |
| I/O read | `io_in_mb` | float | Cumulative bytes read in MiB |
| I/O written | `io_out_mb` | float | Cumulative bytes written in MiB |
| CPU time | `cpu_time_sec` | float | CPU time (user + system) in seconds |

**System state (distress metrics):**

These flag potentially unreliable measurements. A job that ran under memory pressure or I/O contention may have inflated runtimes.

| Field | Key | Type | Capture | Description |
|-------|-----|------|---------|-------------|
| Load average | `load_avg` | float | Job start | 1-min load average. Values >> `cpu_cores` indicate contention. |
| Memory available | `mem_avail_mb` | int | Job start | Available memory in MB |
| Swap used | `swap_used_mb` | int | Job start | Swap in use in MB. Non-zero is a red flag. |
| I/O wait | `io_wait_pct` | float | Delta over job | CPU time % waiting for I/O. Linux only (0 elsewhere). |

### 5. File Entries

Each element in `inputs`/`outputs` arrays:

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | File extension, preserving compound extensions (e.g. `.fastq.gz`). Empty string if none. |
| `size` | int64 | File size in bytes. 0 if not stat-able. |

All files are included (no filtering of index/auxiliary files).

### 6. Tool Inference

The `tool` field identifies the primary scientific tool. Resolution order:

1. **Explicit annotation:** If the rule sets `_psb_tool` (see Section 8), use that value. If the annotation names a generic interpreter, the observation MUST be dropped.
2. **Auto-detection:** Parse the shell template. Skip leading generic interpreter tokens and use the first non-interpreter token. If none remains, drop.
3. **Drop rule:** The observation is dropped whenever the resolved tool is a generic interpreter or auto-detection yields no non-interpreter token.

Generic interpreters: `python`, `python3`, `bash`, `sh`, `Rscript`, `perl`, `java`, `ruby`.

### 7. Shell Block

The `shell_block` field stores the full shell template with Snakemake placeholders (`{input}`, `{output}`, `{threads}`, etc.) unresolved. This avoids path leakage while preserving the complete picture for pipelines and multi-command rules.

This complements `command` (tool/subcommand) and `params` (arguments for the primary command, which may be just one segment of a pipe).

### 8. PSB Annotations

Workflow authors annotate rules via the `_psb_*` param namespace.

**Rule-level (via `params:`):**

| Param key | Observation field | Behavior |
|-----------|-------------------|----------|
| `_psb_tool` | `tool` | Replaces auto-detected tool name |
| `_psb_tool_version` | `tool_version` | Tool version string |
| `_psb_primary_cmd` | `command` | Replaces auto-detected command |
| `_psb_params` | `params` | Replaces auto-detected params |
| `_psb_category` | `category` | Workflow step category |

<!-- INPUT NEEDED: What's the expected format for `_psb_params`? Free-form string? Key-value pairs? -->
<!-- INPUT NEEDED: `_psb_tool_version` — should this be user-specified or auto-resolved from the environment (e.g. via a callback)? If auto-resolved, this becomes a future improvement. -->

**Workflow-level defaults (via `config.yaml`):**

```yaml
psb:
  workflow_url: "https://github.com/org/my-workflow"
  workflow_version: "v1.2.0"
  category: "single-cell clustering"
```

`workflow_url` and `workflow_version` are session-level (Section 3). Other keys are defaults that rule-level params override.

**Precedence:** rule-level `_psb_*` param > workflow config `psb.*` > auto-detection.

**When `_psb_primary_cmd` is set:** auto-detected `params` is cleared (it was derived from the original first shell line). Set `_psb_params` explicitly if needed.

**Examples:**

```python
rule sort_bam:
    params:
        _psb_tool="samtools",
        _psb_primary_cmd="sort",
        _psb_category="alignment",
    shell: "samtools sort -@ {threads} {input} -o {output}"

rule align_and_sort:
    params:
        _psb_tool="samtools",
        _psb_primary_cmd="samtools sort",
        _psb_category="alignment",
    shell:
        "bwa mem -t {threads} {input.ref} {input.r1} {input.r2} | "
        "samtools sort -@ {threads} -o {output}"

# Cross-method comparison via shared category
rule scgpt_normalize:
    params:
        _psb_tool="scgpt",
        _psb_category="normalization",
    shell: "python scripts/scgpt_norm.py {input} {output}"

rule scvi_normalize:
    params:
        _psb_tool="scvi-tools",
        _psb_category="normalization",
    shell: "python scripts/scvi_norm.py {input} {output}"
```

Without annotations, auto-detection on the last two rules would skip `python` and resolve to the script path, which is not meaningful.

### 9. Environment

A fingerprint of the execution platform. The server deduplicates by a deterministic hash.

| Field | Key | Type | Description |
|-------|-----|------|-------------|
| Host hash | `host_hash` | string | SHA256 of hostname |
| CPU model | `cpu_model` | string | e.g. `Intel Xeon E5-2680 v4` |
| CPU features | `cpu_features` | uint64 | Bitmask of curated flags (see Appendix A) |
| CPU cores | `cpu_cores` | int | Physical core count |
| L2 cache | `l2_cache_kb` | int | L2 cache size in KB |
| L3 cache | `l3_cache_kb` | int | L3 cache size in KB |
| CPU frequency | `cpu_freq_mhz` | int | Max CPU frequency in MHz |
| Platform | `os` | string | `linux`, `darwin`, `freebsd`, or `windows` |
| Kernel version | `kernel_version` | string | Numeric kernel version |
| Kernel string | `kernel_string` | string | Full platform string |
| Runtime version | `sm_version` | string | Snakemake version |
| Deployment mode | `deploy_mode` | string | See Appendix B |

**Environment hash:**

```
SHA256("host_hash={host_hash}\ncpu_model={cpu_model}\ncpu_features={cpu_features}\ncpu_cores={cpu_cores}\nl2={l2_cache_kb}\nl3={l3_cache_kb}\nfreq={cpu_freq_mhz}\nos={os}\nkernel={kernel_version}\nkernel_string={kernel_string}\nsm={sm_version}\ndeploy={deploy_mode}")
```

**Deprecated:** `cpu_flags` (raw string). If sent without `cpu_features`, the server auto-encodes to the bitmask.

---

## Part II: Submission Protocol

### 10. Client Responsibilities

#### 10.1 Parameter Sanitization

When `params` is derived from the shell template, it already contains `{input}`/`{output}` placeholders and needs no further sanitization. When `_psb_params` is used, the author controls the content.

The server rejects parameters containing absolute path patterns (`/home/`, `/Users/`, `C:\`, `/tmp/`).

#### 10.2 Opt-in Consent

Telemetry MUST be opt-in. No data is sent unless the user explicitly enables it (e.g. `--share-benchmark`).

#### 10.3 Privacy

- Hostnames are hashed (`host_hash`), never sent raw.
- File paths appear only as template placeholders.
- IP addresses are not stored by the application.

### 11. Wire Format

The client submits observations as **JSON Lines** (`\n`-delimited). Each line inlines session, environment, and observation fields into a single flat JSON object — no nested envelope.

```jsonl
{"session_id":"...","record_id":"...","workflow_url":"...","host_hash":"...","cpu_model":"...","tool":"samtools","runtime_sec":42.3,...}
```

The `session_id` field ties the line to a session. Session-level fields (`workflow_url`, `workflow_version`) are repeated per line but stored once (first wins). Environment fields are repeated per line but deduplicated by hash.

### 12. Transport

```
POST /v1/telemetry
Content-Type: text/jsonl
```

The client accumulates observations locally and submits them as a single HTTP POST at session end. The server splits on `\n` and validates each line independently.

<!-- INPUT NEEDED: Review the 10 MiB payload size limit — is this appropriate for large workflows? -->

**Size limit:** 10 MiB uncompressed. A typical 500-observation session produces ~500 KiB.

**Retry:** Resend the whole payload; duplicates are idempotent. On failure, the buffer is preserved.

**Timeout:** Submission MUST NOT block workflow execution. Fire-and-forget with a short timeout (e.g. 10s).

### 13. Authentication (MVP)

A best-effort triple gate check — NOT cryptographic security, just an abuse deterrent.

| Gate | Mechanism | Header/Cookie |
|------|-----------|---------------|
| 1 | Shared token | `X-PSB-Token: <token>` |
| 2 | Session cookie | `Cookie: _psb_session=<value>` |
| 3 | Nonce | `X-PSB-Nonce: <random string>` |

The token is distributed with the client. It is a speed bump, not a secret. The nonce is per-request. If any gate fails, the server returns `418 I'm a teapot` with no further signal.

### 14. Server Validation

After gate checks, per-line validation:

1. Valid JSON
2. `session_id` non-empty
3. `record_id` non-empty
4. `tool` non-empty
5. `runtime_sec` > 0
6. `params` contains no absolute path patterns

Invalid lines are skipped and reported. The request does not fail due to individual bad lines.

### 15. Duplicate Handling

An observation with the same `(session_id, record_id)` pair as an existing record is counted as a duplicate; the original is preserved. This makes submission idempotent.

### 16. Response Format

```json
{
  "status": "ok",
  "accepted": 47,
  "duplicates": 3,
  "rejected": 2,
  "errors": [
    {"line": 12, "error": "runtime_sec must be > 0"}
  ]
}
```

Success and partial success both return **201 Created**. Gate failure returns **418**.

---

## Part III: Data Retrieval

### 17. Read Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Paginated dashboard |
| GET | `/session/:id` | Session detail |
| GET | `/session/:id/jsonl` | Session as JSON Lines |
| GET | `/session/:id/parquet` | Session as Parquet |
| GET | `/env/:id` | Environment detail |
| GET | `/record/:id` | Observation detail |
| GET | `/record/:id/json` | Observation as JSON |
| GET | `/export/parquet?tool=X&week=YYYY-Www` | Tool aggregate Parquet by ISO week |

### 18. Export Formats

**JSONL** (`/session/:id/jsonl`): One line per observation: `{"observation": {...}, "environment": {...}, "session": {...}}`.

**JSON** (`/record/:id/json`): Same structure, single object.

**Parquet** (`/session/:id/parquet`, `/export/parquet`): Flat table, one row per observation. Environment and session fields merged in. CPU features decoded to comma-separated string. Input/output arrays as JSON strings alongside computed scalar totals.

---

## Part IV: Security

### 19. Threat Model (MVP)

| Threat | MVP Mitigation | Production Mitigation |
|--------|----------------|----------------------|
| Spam | Triple gate check | API keys + rate limiting |
| Path leakage | Server rejects absolute paths | Client stripping + server validation |
| Hostname fingerprinting | SHA256 hash | Same |
| Replay | Duplicate detection by `(session_id, record_id)` | Timestamped nonces with expiry |
| Data poisoning | Trusted community model | Statistical outlier detection |
| DDoS | Rate limiting | CDN, IP reputation |
| URL abuse | Server strips query/fragment/auth, rejects non-http(s) | Same |

### 20. Future Authentication

Production deployments SHOULD replace the shared token with per-user API keys, OAuth2, and per-key rate limiting.

---

## Appendices

### A. CPU Features Bitmask

The `cpu_features` field encodes curated flags into a `uint64`. Bit assignments must be identical in client and server.

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
| 12 | avx512_vnni | x86 SIMD (ML) |
| 13 | f16c | x86 half-precision |
| 14 | popcnt | Bit manipulation |
| 15 | bmi1 | Bit manipulation |
| 16 | bmi2 | Bit manipulation |
| 17 | abm/lzcnt | Bit manipulation |
| 18 | aes | Crypto |
| 19 | sha | Crypto |
| 20 | pclmulqdq | Crypto (checksums) |
| 21 | rdrand | Hardware RNG |
| 22 | tsx | Transactional memory |
| 23 | neon/asimd | ARM SIMD |
| 24 | sve | ARM scalable vectors |
| 25 | sve2 | ARM scalable vectors |
| 26 | crc32 | CRC |
| 27 | xop | AMD |

Multiple OS flag names may map to the same bit (e.g. `pni`/`sse3` → bit 1, `asimd`/`neon` → bit 23).

### B. Deployment Mode

Reflects Snakemake `--software-deployment-method`:

| Value | Meaning |
|-------|---------|
| `host` | No deployment method (bare metal) |
| `conda` | Conda environments |
| `apptainer` | Apptainer/Singularity containers |
| `env_modules` | Environment modules |

<!-- INPUT NEEDED: Revisit the sorting order for multiple deployment methods. Should be nesting order (e.g. conda wrapping apptainer), not alphabetical. -->

When multiple methods are active, joined with `+` in sorted order (e.g. `conda+apptainer`).

### C. Database Indexes

| Table | Index | Type |
|-------|-------|------|
| `sessions` | `session_id` | Unique |
| `environments` | `hash` | Unique |
| `execution_metrics` | `(session_id, record_id)` | Unique composite |
| `execution_metrics` | `tool` | Non-unique |

### D. API Versioning

The API is versioned via URL path prefix (`/v1/`). Breaking changes require a new version. The `sm_version` environment field allows version-specific parsing.

### E. Future Work

- Compression (gzip request bodies)
- Streaming submission (during workflow, not just at end)
- Aggregation API (percentiles, distributions per tool)
- Public comparison dashboard
- Signed submissions (Ed25519)
- Workflow-level aggregation (DAG hash)
- Rule name capture alongside tool
- Non-zero exit code recording
- Periodic background flushes

### F. Open Questions

Items needing discussion or design decisions before stabilizing the spec.

- **`tool_version` resolution.** Currently user-specified via `_psb_tool_version` annotation, but this is fragile — users will forget to update it, or hard-code stale values. The right approach is probably a hook/callback mechanism: the client calls a user-provided function (or runs a shell command like `samtools --version`) to resolve the version from the runtime environment. This decouples version discovery from the workflow specification. Until then, `_psb_tool_version` is a stopgap.

- **`_psb_params` format.** Currently free-form string. Should this be structured (e.g. key-value pairs)? Free-form is simpler and matches how shell arguments work, but structured params would enable better downstream aggregation (e.g. grouping by `-@ 4` vs `-@ 8`). May not be worth the complexity for MVP.

- **`category` vs `tags`.** A single `category` string is limiting. A `tags` list (e.g. `["alignment", "single-cell", "10x"]`) would allow richer cross-method queries. But tags need governance (free-form tags drift quickly). Consider a controlled vocabulary or at least a recommended tag list.

- **Deployment mode ordering.** When multiple methods are active (e.g. `conda+apptainer`), the current alphabetical join doesn't reflect nesting semantics. `conda` wrapping `apptainer` is different from the reverse. Need to decide if ordering matters for analysis and, if so, how to capture nesting.

- **Payload size limit.** 10 MiB may be tight for very large workflows (thousands of rules with many files each). Need real-world data to calibrate.

- **Interpreter skip heuristic.** Auto-detection skips leading interpreter tokens (`python`, `bash`, etc.) to find the "real" tool. This works for `python scripts/foo.py` but produces odd results for `bash -c "complex command"` (tool becomes `-c`). May need a smarter heuristic or just rely on annotations for non-trivial cases.
