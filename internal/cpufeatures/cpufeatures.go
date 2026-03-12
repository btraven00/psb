// Package cpufeatures defines a bitmask encoding for curated CPU feature flags
// relevant to scientific computing workloads. The same bit positions must be
// used by both the Python telemetry client and the Go server.
//
// The curated set covers SIMD, cryptography, bit manipulation, and
// hardware-accelerated math — features that meaningfully affect performance
// of tools commonly orchestrated by Snakemake across domains (genomics,
// physics, ML preprocessing, image processing, etc.).
package cpufeatures

import (
	"sort"
	"strings"
)

// Feature represents a single CPU feature flag as a bitmask bit.
type Feature uint64

const (
	// x86 SIMD
	SSE2       Feature = 1 << iota // 0
	SSE3                           // 1
	SSSE3                          // 2
	SSE41                          // 3
	SSE42                          // 4
	AVX                            // 5
	AVX2                           // 6
	FMA                            // 7
	AVX512F                        // 8
	AVX512BW                       // 9
	AVX512VL                       // 10
	AVX512DQ                       // 11
	AVX512VNNI                     // 12 - neural network instructions (ML inference)
	F16C                           // 13 - half-precision float conversion

	// Bit manipulation
	POPCNT // 14
	BMI1   // 15
	BMI2   // 16
	ABM    // 17 - advanced bit manipulation (LZCNT)

	// Crypto / hashing
	AES    // 18
	SHA    // 19
	PCLMUL // 20 - carry-less multiplication (checksums)

	// Misc x86
	RDRAND // 21 - hardware RNG
	TSX    // 22 - transactional memory

	// ARM
	NEON  // 23
	SVE   // 24
	SVE2  // 25
	CRC32 // 26

	// AMD-specific
	XOP // 27

	_featureCount // sentinel — not a real feature
)

// entry maps a /proc/cpuinfo (or sysctl) flag name to a Feature bit.
type entry struct {
	name    string
	feature Feature
}

// Registry is the ordered list of (flag-name, bit) pairs. The Python client
// must use the identical list.
var Registry = []entry{
	// x86 SIMD
	{"sse2", SSE2},
	{"pni", SSE3},  // Linux reports SSE3 as "pni"
	{"sse3", SSE3}, // macOS / alternative
	{"ssse3", SSSE3},
	{"sse4_1", SSE41},
	{"sse4_2", SSE42},
	{"avx", AVX},
	{"avx2", AVX2},
	{"fma", FMA},
	{"avx512f", AVX512F},
	{"avx512bw", AVX512BW},
	{"avx512vl", AVX512VL},
	{"avx512dq", AVX512DQ},
	{"avx512_vnni", AVX512VNNI},
	{"avx512vnni", AVX512VNNI}, // alternative
	{"f16c", F16C},

	// Bit manipulation
	{"popcnt", POPCNT},
	{"bmi1", BMI1},
	{"bmi2", BMI2},
	{"abm", ABM},
	{"lzcnt", ABM}, // LZCNT is part of ABM

	// Crypto / hashing
	{"aes", AES},
	{"aesni", AES},
	{"sha_ni", SHA},
	{"sha1", SHA},
	{"sha2", SHA},
	{"pclmulqdq", PCLMUL},
	{"pclmul", PCLMUL},

	// Misc x86
	{"rdrand", RDRAND},
	{"rtm", TSX},
	{"hle", TSX},

	// ARM
	{"neon", NEON},
	{"asimd", NEON},
	{"sve", SVE},
	{"sve2", SVE2},
	{"crc32", CRC32},

	// AMD
	{"xop", XOP},
}

// bitToName maps each Feature bit to its canonical display name.
var bitToName = map[Feature]string{
	SSE2:       "sse2",
	SSE3:       "sse3",
	SSSE3:      "ssse3",
	SSE41:      "sse4_1",
	SSE42:      "sse4_2",
	AVX:        "avx",
	AVX2:       "avx2",
	FMA:        "fma",
	AVX512F:    "avx512f",
	AVX512BW:   "avx512bw",
	AVX512VL:   "avx512vl",
	AVX512DQ:   "avx512dq",
	AVX512VNNI: "avx512_vnni",
	F16C:       "f16c",
	POPCNT:     "popcnt",
	BMI1:       "bmi1",
	BMI2:       "bmi2",
	ABM:        "abm",
	AES:        "aes",
	SHA:        "sha",
	PCLMUL:     "pclmulqdq",
	RDRAND:     "rdrand",
	TSX:        "tsx",
	NEON:       "neon",
	SVE:        "sve",
	SVE2:       "sve2",
	CRC32:      "crc32",
	XOP:        "xop",
}

// Encode converts a comma-separated list of CPU flag names (as found in
// /proc/cpuinfo) into a bitmask of recognised features.
func Encode(flags string) uint64 {
	nameMap := make(map[string]Feature, len(Registry))
	for _, e := range Registry {
		nameMap[e.name] = e.feature
	}
	var mask uint64
	for _, f := range strings.Split(strings.ToLower(flags), ",") {
		f = strings.TrimSpace(f)
		if bit, ok := nameMap[f]; ok {
			mask |= uint64(bit)
		}
	}
	return mask
}

// Decode converts a bitmask back into a sorted, comma-separated list of
// canonical feature names.
func Decode(mask uint64) string {
	type kv struct {
		bit  Feature
		name string
	}
	var pairs []kv
	for bit, name := range bitToName {
		pairs = append(pairs, kv{bit, name})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].bit < pairs[j].bit })

	var out []string
	for _, p := range pairs {
		if mask&uint64(p.bit) != 0 {
			out = append(out, p.name)
		}
	}
	return strings.Join(out, ",")
}
