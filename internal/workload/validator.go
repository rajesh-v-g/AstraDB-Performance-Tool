package workload

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ValidationResult is the outcome of validating a YAML workload string.
type ValidationResult struct {
	// Valid is true when both validation tiers pass.
	Valid bool `json:"valid"`
	// Error is a human-readable description of the first error found. Empty when Valid.
	Error string `json:"error,omitempty"`
	// Tier identifies which tier caught the error: "syntax" (Tier 1) or "structure" (Tier 2).
	Tier string `json:"tier,omitempty"`
}

// validationYAML is a minimal structure used only for the Tier 2 structure check.
type validationYAML struct {
	Blocks map[string]struct {
		Ops interface{} `yaml:"ops"`
	} `yaml:"blocks"`
}

// Validate runs a two-tier validation on yamlText:
//
//	Tier 1 — YAML syntax (yaml.Unmarshal)
//	Tier 2 — structural check: "blocks" key present, at least one block has ops
func Validate(yamlText string) ValidationResult {
	// Tier 1 — syntax.
	var raw interface{}
	if err := yaml.Unmarshal([]byte(yamlText), &raw); err != nil {
		return ValidationResult{
			Valid: false,
			Error: fmt.Sprintf("YAML syntax error: %v", err),
			Tier:  "syntax",
		}
	}

	// Tier 2 — structure.
	var doc validationYAML
	if err := yaml.Unmarshal([]byte(yamlText), &doc); err != nil {
		return ValidationResult{
			Valid: false,
			Error: fmt.Sprintf("YAML structure error: %v", err),
			Tier:  "structure",
		}
	}
	if len(doc.Blocks) == 0 {
		return ValidationResult{
			Valid: false,
			Error: "workload must have a 'blocks' key with at least one block",
			Tier:  "structure",
		}
	}
	hasOps := false
	for _, b := range doc.Blocks {
		if b.Ops != nil {
			hasOps = true
			break
		}
	}
	if !hasOps {
		return ValidationResult{
			Valid: false,
			Error: "at least one block must have an 'ops' key",
			Tier:  "structure",
		}
	}

	return ValidationResult{Valid: true}
}
