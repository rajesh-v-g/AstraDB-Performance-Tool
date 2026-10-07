package binding_test

import (
	"strings"
	"testing"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/binding"
)

func TestCompile_LiteralOnly(t *testing.T) {
	tmpl, err := binding.Compile("SELECT 1")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	exec := tmpl.NewExecutor(1000, 64)
	got := exec.Execute(0)
	if got != "SELECT 1" {
		t.Errorf("Execute = %q, want %q", got, "SELECT 1")
	}
}

func TestCompile_SeqKey_Deterministic(t *testing.T) {
	tmpl, err := binding.Compile("INSERT INTO t (k) VALUES ({seq_key})")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	exec := tmpl.NewExecutor(1000, 64)
	r1 := exec.Execute(42)
	r2 := exec.Execute(42)
	if r1 != r2 {
		t.Errorf("seq_key not deterministic: %q vs %q", r1, r2)
	}
	if !strings.Contains(r1, "'key-42'") {
		t.Errorf("seq_key(42) should contain \"'key-42'\", got %q", r1)
	}
}

func TestCompile_RWKey_InBounds(t *testing.T) {
	tmpl, err := binding.Compile("SELECT * FROM t WHERE k={rw_key}")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	maxKeys := int64(100)
	exec := tmpl.NewExecutor(maxKeys, 64)
	for i := 0; i < 200; i++ {
		got := exec.Execute(int64(i))
		// Should contain 'key-N' where N is in [0, maxKeys).
		if !strings.HasPrefix(got, "SELECT * FROM t WHERE k='key-") {
			t.Errorf("rw_key produced unexpected output: %q", got)
		}
	}
}

func TestCompile_NoBracesInOutput(t *testing.T) {
	rawCQL := "INSERT INTO t (k, v) VALUES ({seq_key}, {seq_value})"
	tmpl, err := binding.Compile(rawCQL)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	exec := tmpl.NewExecutor(1000, 32)
	for i := 0; i < 10; i++ {
		out := exec.Execute(int64(i))
		if strings.Contains(out, "{") || strings.Contains(out, "}") {
			t.Errorf("Execute(%d) still has braces: %q", i, out)
		}
	}
}

func TestCompile_UnknownToken(t *testing.T) {
	_, err := binding.Compile("SELECT {unknown_token} FROM t")
	if err == nil {
		t.Error("expected error for unknown token, got nil")
	}
}

func TestCompile_IndexedBatchTokens(t *testing.T) {
	raw := "BEGIN BATCH INSERT INTO t (k,v) VALUES ({seq_key0}, {seq_value0}) APPLY BATCH"
	tmpl, err := binding.Compile(raw)
	if err != nil {
		t.Fatalf("Compile batch tokens: %v", err)
	}
	exec := tmpl.NewExecutor(1000, 32)
	out := exec.Execute(5)
	if strings.Contains(out, "{") {
		t.Errorf("indexed batch token not resolved: %q", out)
	}
	// seq_key0 at counter=5 → batchOffset=0 → effective counter=5 → "'key-5'"
	if !strings.Contains(out, "'key-5'") {
		t.Errorf("expected 'key-5' in seq_key0(5), got: %q", out)
	}
}

func TestCompile_UUID(t *testing.T) {
	tmpl, err := binding.Compile("INSERT INTO t (id) VALUES ({uuid})")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	exec := tmpl.NewExecutor(100, 64)
	out1 := exec.Execute(0)
	out2 := exec.Execute(0)
	if out1 == out2 {
		t.Error("UUID should be different on consecutive calls")
	}
}

func TestCompile_MultipleExecutorsIndependent(t *testing.T) {
	tmpl, err := binding.Compile("{seq_key}")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	e1 := tmpl.NewExecutor(1000, 64)
	e2 := tmpl.NewExecutor(1000, 64)
	// Both should produce 'key-0' for counter=0 (seq_key is deterministic).
	if e1.Execute(0) != e2.Execute(0) {
		t.Error("two executors produced different seq_key(0)")
	}
}
