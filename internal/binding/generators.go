package binding

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
)

// generatorFn is the signature every generator function must implement.
// counter is the monotonically increasing op counter for this worker.
// e is the calling Executor (provides maxKeys, valueSize, and private rand).
// b is the output builder to write into.
type generatorFn func(counter int64, e *Executor, b *strings.Builder)

// generators maps token names to their generator functions.
var generators = map[string]generatorFn{
	"seq_key":          genSeqKey,
	"rw_key":           genRWKey,
	"seq_value":        genSeqValue,
	"rw_value":         genRWValue,
	"uuid":             genUUID,
	"timestamp":        genTimestamp,
	"seq_device_id":    genSeqDeviceID,
	"rw_device_id":     genRWDeviceID,
	"seq_sensor_value": genSeqSensorValue,
	"rw_sensor_value":  genRWSensorValue,
}

func genSeqKey(counter int64, _ *Executor, b *strings.Builder) {
	fmt.Fprintf(b, "'key-%d'", counter)
}

func genRWKey(counter int64, e *Executor, b *strings.Builder) {
	k := e.rng.Int63n(maxOrOne(e.maxKeys))
	fmt.Fprintf(b, "'key-%d'", k)
}

func genSeqValue(counter int64, e *Executor, b *strings.Builder) {
	b.WriteByte('\'')
	b.WriteString(pseudoRandString(e.valueSize, counter))
	b.WriteByte('\'')
}

func genRWValue(_ int64, e *Executor, b *strings.Builder) {
	b.WriteByte('\'')
	b.WriteString(randString(e.rng, e.valueSize))
	b.WriteByte('\'')
}

func genUUID(_ int64, _ *Executor, b *strings.Builder) {
	// UUIDs are unquoted in CQL — they are a native literal type.
	b.WriteString(uuid.New().String())
}

func genTimestamp(_ int64, _ *Executor, b *strings.Builder) {
	// Timestamps are written as integer literals (milliseconds since epoch).
	fmt.Fprintf(b, "%d", time.Now().UnixMilli())
}

func genSeqDeviceID(counter int64, e *Executor, b *strings.Builder) {
	numDevices := maxOrOne(e.maxKeys)
	fmt.Fprintf(b, "%d", counter%numDevices)
}

func genRWDeviceID(_ int64, e *Executor, b *strings.Builder) {
	fmt.Fprintf(b, "%d", e.rng.Int63n(maxOrOne(e.maxKeys)))
}

func genSeqSensorValue(counter int64, _ *Executor, b *strings.Builder) {
	// Deterministic per counter — seeded from counter.
	r := rand.New(rand.NewSource(counter)) //nolint:gosec
	fmt.Fprintf(b, "%.4f", r.Float64()*100.0)
}

func genRWSensorValue(_ int64, e *Executor, b *strings.Builder) {
	fmt.Fprintf(b, "%.4f", e.rng.Float64()*100.0)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// randString generates a random ASCII string of length n using the provided rng.
func randString(r *rand.Rand, n int) string {
	if n <= 0 {
		n = 64
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[r.Intn(len(charset))]
	}
	return string(b)
}

// pseudoRandString generates a deterministic ASCII string seeded from counter.
func pseudoRandString(n int, counter int64) string {
	if n <= 0 {
		n = 64
	}
	r := rand.New(rand.NewSource(counter)) //nolint:gosec
	return randString(r, n)
}

// maxOrOne returns v if v > 0, otherwise 1 (prevents division/modulo by zero).
func maxOrOne(v int64) int64 {
	if v <= 0 {
		return 1
	}
	return v
}
