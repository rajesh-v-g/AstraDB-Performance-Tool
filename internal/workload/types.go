// Package workload defines the shared workload data model and provides YAML loading,
// validation, and the unified workload registry.
package workload

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/binding"
)

// WorkloadDef is the complete definition of a workload — either loaded from a YAML file
// or registered directly as a Go value.
type WorkloadDef struct {
	// ID is the unique identifier for the workload (slugified name).
	ID string `json:"id"`
	// Label is the human-readable display name.
	Label string `json:"label"`
	// Filename is the base filename (YAML workloads only).
	Filename string `json:"filename,omitempty"`
	// Path is the absolute path to the YAML file (YAML workloads only).
	Path string `json:"-"`
	// Source is one of "yaml-builtin", "yaml-custom", or "builtin-go".
	Source string `json:"source"`
	// Description is the human-readable description from the YAML header.
	Description string `json:"description,omitempty"`
	// Phases lists the phase names present in this workload (e.g. ["schema","rampup","main","truncate"]).
	Phases []string `json:"phases"`
	// Parameters is the map of named parameters with their defaults and types.
	Parameters map[string]Parameter `json:"parameters,omitempty"`
	// Blocks is the ordered list of execution blocks. Not serialised to clients.
	Blocks []Block `json:"-"`
}

// Parameter describes a single named template parameter with its default and type.
type Parameter struct {
	// Default is the default string value.
	Default string `json:"default"`
	// Type is "string" or "number".
	Type string `json:"type"`
	// Label is the human-readable form label.
	Label string `json:"label"`
}

// Block is a named group of CQL operations within a workload.
type Block struct {
	// Name is the block name as declared in the YAML (e.g. "rampup", "main-read").
	Name string
	// Ratio controls the weighted frequency of this block relative to others in the same phase.
	Ratio int
	// Prepared indicates whether queries in this block should use prepared statements.
	Prepared bool
	// Ops is the ordered list of operations in this block.
	Ops []Op
}

// Op is a single CQL operation within a block.
type Op struct {
	// Name is the optional operation name (map key in YAML, or auto-generated).
	Name string
	// RawCQL is the CQL string as written — may contain {token} binding placeholders
	// and TEMPLATE(name,default) macro syntax.
	RawCQL string
	// Template is the pre-compiled binding template. Nil until CompileTemplates is called.
	Template *binding.Template
}

// templateRe matches TEMPLATE(name,default) macros in CQL strings.
var templateRe = regexp.MustCompile(`TEMPLATE\(([^,)]+),([^)]+)\)`)

// CompileTemplates resolves template tokens in all block ops and compiles each
// resolved CQL string into a binding.Template.
//
// Two resolution passes run in order:
//  1. TEMPLATE(name,default) macros (YAML-style): replaced with extraParams[name]
//     or the macro default.
//  2. {paramName} tokens that appear in the workload's Parameters map: replaced
//     with extraParams[paramName] or the Parameter.Default. This handles Go-native
//     workloads that embed parameter tokens directly in CQL strings.
//
// After both passes, the remaining {token} placeholders must be registered
// binding generators (seq_key, rw_key, etc.). Unknown tokens cause an error.
// CompileTemplates must be called before any Op.Template is used for execution.
func (wd *WorkloadDef) CompileTemplates(extraParams map[string]string) error {
	for bi := range wd.Blocks {
		for oi := range wd.Blocks[bi].Ops {
			op := &wd.Blocks[bi].Ops[oi]
			// Pass 1: TEMPLATE(name,default) macros.
			resolved := resolveTemplates(op.RawCQL, extraParams)
			// Pass 2: {paramName} tokens from the Parameters map.
			resolved = resolveParamTokens(resolved, wd.Parameters, extraParams)
			t, err := binding.Compile(resolved)
			if err != nil {
				return fmt.Errorf("workload %q block %q op %q: %w",
					wd.ID, wd.Blocks[bi].Name, op.Name, err)
			}
			op.Template = t
		}
	}
	return nil
}

// resolveParamTokens replaces {paramName} tokens that are declared in the workload's
// Parameters map with their runtime or default values. Tokens not in Parameters are
// left untouched so that binding.Compile can handle them as generator tokens.
func resolveParamTokens(s string, params map[string]Parameter, extraParams map[string]string) string {
	if len(params) == 0 {
		return s
	}
	result := s
	for name, param := range params {
		token := "{" + name + "}"
		val := param.Default
		if v, ok := extraParams[name]; ok && v != "" {
			val = v
		}
		result = strings.ReplaceAll(result, token, val)
	}
	return result
}

// resolveTemplates replaces all TEMPLATE(name,default) macros in s with the
// value from extraParams[name], or with the default when the key is absent.
func resolveTemplates(s string, extraParams map[string]string) string {
	return templateRe.ReplaceAllStringFunc(s, func(match string) string {
		sub := templateRe.FindStringSubmatch(match)
		if len(sub) != 3 {
			return match
		}
		name := strings.TrimSpace(sub[1])
		def := strings.TrimSpace(sub[2])
		if v, ok := extraParams[name]; ok && v != "" {
			return v
		}
		return def
	})
}
