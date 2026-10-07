package workload_test

import (
	"testing"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"
)

const validYAML = `
description: A test workload.
blocks:
  rampup:
    ops:
      insert: INSERT INTO ks.t (k, v) VALUES ('key', 'val')
`

const syntaxErrorYAML = `
description: |
  bad yaml
blocks:
  rampup:
    ops: [
      not closed
`

const missingBlocksYAML = `
description: No blocks here.
`

const noOpsYAML = `
description: Has blocks but no ops.
blocks:
  rampup:
    params:
      prepared: true
`

func TestValidate_Valid(t *testing.T) {
	res := workload.Validate(validYAML)
	if !res.Valid {
		t.Errorf("expected Valid=true, got error=%q tier=%q", res.Error, res.Tier)
	}
}

func TestValidate_SyntaxError(t *testing.T) {
	res := workload.Validate(syntaxErrorYAML)
	if res.Valid {
		t.Error("expected Valid=false for syntax error")
	}
	if res.Tier != "syntax" {
		t.Errorf("Tier = %q, want %q", res.Tier, "syntax")
	}
}

func TestValidate_MissingBlocks(t *testing.T) {
	res := workload.Validate(missingBlocksYAML)
	if res.Valid {
		t.Error("expected Valid=false for missing blocks")
	}
	if res.Tier != "structure" {
		t.Errorf("Tier = %q, want %q", res.Tier, "structure")
	}
}

func TestValidate_NoOps(t *testing.T) {
	res := workload.Validate(noOpsYAML)
	if res.Valid {
		t.Error("expected Valid=false for no ops")
	}
	if res.Tier != "structure" {
		t.Errorf("Tier = %q, want %q", res.Tier, "structure")
	}
}
