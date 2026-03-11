#!/usr/bin/env bash
# Inject test fixtures into the PSB server using batch JSONL POST.
# Usage: ./scripts/fixtures.sh [base_url]
#   e.g. ./scripts/fixtures.sh http://localhost:8080

set -euo pipefail

BASE="${1:-http://localhost:8080}"
TOKEN="${PSB_TOKEN:-dev-secret}"

# Generate session IDs (simulating two workflow runs)
SESSION_A="ses_$(date +%s)_alpha"
SESSION_B="ses_$(date +%s)_beta"

make_record_id() {
  echo -n "${1}_${2}" | sha256sum | cut -c1-16
}

# Build a JSONL payload in a temp file
PAYLOAD=$(mktemp)
trap 'rm -f "$PAYLOAD"' EXIT

echo "building fixtures ..."
echo "  session_a: $SESSION_A"
echo "  session_b: $SESSION_B"
echo ""

# --- Session A: Linux Intel workstation, samtools sort x10 ---
for i in $(seq 1 10); do
  rid=$(make_record_id "samtools_sort" "$i")
  cat <<EOF >> "$PAYLOAD"
{"session_id":"$SESSION_A","record_id":"$rid","host_hash":"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2","cpu_model":"Intel Xeon E5-2680 v4","cpu_flags":"avx2,sse4_2,popcnt","os":"linux","kernel_version":"6.1.0","kernel_string":"Linux 6.1.0-18-amd64","sm_version":"8.20.0","deploy_mode":"conda","tool":"samtools","command":"sort","params":"-@ 4 -m 2G","input_size":$((RANDOM * 1000 + 500000)),"input_type":".bam","runtime_sec":$(echo "scale=2; $RANDOM / 1000 + 5" | bc),"max_rss_mb":$(echo "scale=1; $RANDOM / 100 + 200" | bc),"cpu_percent":$(echo "scale=1; $RANDOM / 500 + 50" | bc),"exit_code":0}
EOF
done

# --- Session B: macOS Apple Silicon, bwa mem x8 ---
for i in $(seq 1 8); do
  rid=$(make_record_id "bwa_mem" "$i")
  cat <<EOF >> "$PAYLOAD"
{"session_id":"$SESSION_B","record_id":"$rid","host_hash":"b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3","cpu_model":"Apple M2 Pro","cpu_flags":"neon,fp16","os":"darwin","kernel_version":"23.4.0","kernel_string":"Darwin 23.4.0","sm_version":"8.18.2","deploy_mode":"docker","tool":"bwa","command":"mem","params":"-t 8 -M","input_size":$((RANDOM * 5000 + 1000000)),"input_type":".fastq.gz","runtime_sec":$(echo "scale=2; $RANDOM / 500 + 20" | bc),"max_rss_mb":$(echo "scale=1; $RANDOM / 50 + 1000" | bc),"cpu_percent":$(echo "scale=1; $RANDOM / 400 + 70" | bc),"exit_code":0}
EOF
done

# --- Session A: more tools ---
for tool_data in \
  'fastp|fastp|--qualified_quality_phred 20 --length_required 50|.fastq.gz' \
  'picard|MarkDuplicates|--REMOVE_DUPLICATES true|.bam' \
  'bcftools|call|--multiallelic-caller --variants-only|.vcf.gz' \
  'fastqc|fastqc|--threads 4|.fastq.gz' \
  'gatk|HaplotypeCaller|--emit-ref-confidence GVCF|.bam' \
  'minimap2|minimap2|-ax sr|.fastq.gz' \
  'bedtools|intersect|-a -b -wa|.bed' \
  'deeptools|bamCoverage|--normalizeUsing RPKM --binSize 50|.bam'
do
  IFS='|' read -r tool cmd params itype <<< "$tool_data"
  for i in $(seq 1 4); do
    rid=$(make_record_id "${tool}_${cmd}" "$i")
    cat <<EOF >> "$PAYLOAD"
{"session_id":"$SESSION_A","record_id":"$rid","host_hash":"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2","cpu_model":"Intel Xeon E5-2680 v4","cpu_flags":"avx2,sse4_2,popcnt","os":"linux","kernel_version":"6.1.0","kernel_string":"Linux 6.1.0-18-amd64","sm_version":"8.20.0","deploy_mode":"conda","tool":"$tool","command":"$cmd","params":"$params","input_size":$((RANDOM * 2000 + 100000)),"input_type":"$itype","runtime_sec":$(echo "scale=2; $RANDOM / 800 + 3" | bc),"max_rss_mb":$(echo "scale=1; $RANDOM / 80 + 100" | bc),"cpu_percent":$(echo "scale=1; $RANDOM / 600 + 30" | bc),"exit_code":0}
EOF
  done
done

# --- Session A: failure records ---
rid=$(make_record_id "samtools_view_fail" "1")
cat <<EOF >> "$PAYLOAD"
{"session_id":"$SESSION_A","record_id":"$rid","host_hash":"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2","cpu_model":"Intel Xeon E5-2680 v4","cpu_flags":"avx2,sse4_2,popcnt","os":"linux","kernel_version":"6.1.0","kernel_string":"Linux 6.1.0-18-amd64","sm_version":"8.20.0","deploy_mode":"conda","tool":"samtools","command":"view","params":"-b -q 30","input_size":42000,"input_type":".bam","runtime_sec":0.8,"max_rss_mb":15.2,"cpu_percent":12.0,"exit_code":1}
EOF

rid=$(make_record_id "gatk_oom" "1")
cat <<EOF >> "$PAYLOAD"
{"session_id":"$SESSION_B","record_id":"$rid","host_hash":"b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3","cpu_model":"Apple M2 Pro","cpu_flags":"neon,fp16","os":"darwin","kernel_version":"23.4.0","kernel_string":"Darwin 23.4.0","sm_version":"8.18.2","deploy_mode":"docker","tool":"gatk","command":"HaplotypeCaller","params":"--emit-ref-confidence GVCF","input_size":95000000,"input_type":".bam","runtime_sec":342.1,"max_rss_mb":15800.0,"cpu_percent":98.5,"exit_code":137}
EOF

# --- Duplicate test: re-send the first samtools sort ---
rid=$(make_record_id "samtools_sort" "1")
cat <<EOF >> "$PAYLOAD"
{"session_id":"$SESSION_A","record_id":"$rid","host_hash":"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2","cpu_model":"Intel Xeon E5-2680 v4","cpu_flags":"avx2,sse4_2,popcnt","os":"linux","kernel_version":"6.1.0","kernel_string":"Linux 6.1.0-18-amd64","sm_version":"8.20.0","deploy_mode":"conda","tool":"samtools","command":"sort","params":"-@ 4 -m 2G","input_size":999999,"input_type":".bam","runtime_sec":99.9,"max_rss_mb":999.0,"cpu_percent":99.0,"exit_code":0}
EOF

LINES=$(wc -l < "$PAYLOAD")
echo "submitting $LINES records in a single batch POST ..."
echo ""

curl -s -w "\nHTTP %{http_code}\n" \
  -X POST "$BASE/v1/telemetry" \
  -H 'Content-Type: text/jsonl' \
  -H "X-PSB-Token: $TOKEN" \
  -H "X-PSB-Nonce: fixture-$(date +%s)" \
  -b '_psb_session=fixture-session' \
  --data-binary "@$PAYLOAD"

echo ""
echo "done. view at: $BASE/"
