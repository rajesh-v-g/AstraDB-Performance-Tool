// Package binding provides a pre-compiled CQL template engine.
// Templates are compiled once at workload load time; the hot execution path
// calls Executor.Execute which iterates pre-built segments — no regex, no string
// scanning, no per-call allocations beyond the output string builder reset.
package binding

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// segment is a single compiled unit of a CQL template — either a literal string
// or a generated value.
type segment interface {
	write(counter int64, e *Executor, b *strings.Builder)
}

// literalSegment writes a fixed string.
type literalSegment struct{ s string }

func (l *literalSegment) write(_ int64, _ *Executor, b *strings.Builder) {
	b.WriteString(l.s)
}

// generatorSegment calls a generator function to produce a value.
type generatorSegment struct {
	fn          generatorFn
	batchOffset int64 // non-zero for indexed batch tokens like {seq_key0}
}

func (g *generatorSegment) write(counter int64, e *Executor, b *strings.Builder) {
	g.fn(counter+g.batchOffset, e, b)
}

// Template is a pre-compiled CQL string ready for fast execution.
type Template struct {
	segments []segment
}

// Compile parses rawCQL once and builds a Template from its segments.
// Returns an error for any unknown {token} found in rawCQL.
func Compile(rawCQL string) (*Template, error) {
	var segs []segment
	s := rawCQL
	for {
		start := strings.Index(s, "{")
		if start == -1 {
			// No more tokens — rest is a literal.
			if len(s) > 0 {
				segs = append(segs, &literalSegment{s})
			}
			break
		}
		end := strings.Index(s[start:], "}")
		if end == -1 {
			// Unclosed brace — treat rest as literal.
			segs = append(segs, &literalSegment{s})
			break
		}
		end += start // absolute position in s

		// Emit literal before the token.
		if start > 0 {
			segs = append(segs, &literalSegment{s[:start]})
		}

		token := s[start+1 : end]

		// Check for indexed batch tokens: seq_keyN, rw_keyN, seq_valueN, rw_valueN
		// where N is a single digit 0-9.
		genSeg, err := resolveToken(token)
		if err != nil {
			return nil, err
		}
		segs = append(segs, genSeg)

		s = s[end+1:]
	}
	return &Template{segments: segs}, nil
}

// resolveToken maps a token name to a generatorSegment.
func resolveToken(token string) (*generatorSegment, error) {
	// Try direct lookup first.
	if fn, ok := generators[token]; ok {
		return &generatorSegment{fn: fn}, nil
	}

	// Try indexed batch token: seq_key0..seq_key9, rw_key0..rw_key9,
	// seq_value0..seq_value9, rw_value0..rw_value9.
	if len(token) > 1 {
		lastChar := token[len(token)-1]
		if lastChar >= '0' && lastChar <= '9' {
			base := token[:len(token)-1]
			offset := int64(lastChar - '0')
			if fn, ok := generators[base]; ok {
				return &generatorSegment{fn: fn, batchOffset: offset}, nil
			}
		}
	}

	return nil, fmt.Errorf("binding: unknown token {%s}", token)
}

// Executor is a per-goroutine execution context for a Template.
// Each Executor has its own private rand.Rand — zero shared state, no mutex.
type Executor struct {
	tmpl      *Template
	rng       *rand.Rand
	maxKeys   int64
	valueSize int
	buf       strings.Builder
}

// NewExecutor creates an Executor for tmpl with a private random source.
// maxKeys controls the upper bound for random key generation.
// valueSize controls the length of generated value strings.
func (t *Template) NewExecutor(maxKeys int64, valueSize int) *Executor {
	seed := time.Now().UnixNano() ^ (rand.Int63() ^ int64(maxKeys)) //nolint:gosec
	return &Executor{
		tmpl:      t,
		rng:       rand.New(rand.NewSource(seed)), //nolint:gosec
		maxKeys:   maxKeys,
		valueSize: valueSize,
	}
}

// Execute applies the template to counter and returns the rendered CQL string.
// The internal strings.Builder is reused across calls — no per-call allocation.
func (e *Executor) Execute(counter int64) string {
	e.buf.Reset()
	for _, seg := range e.tmpl.segments {
		seg.write(counter, e, &e.buf)
	}
	return e.buf.String()
}
