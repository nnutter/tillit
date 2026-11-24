package verifier_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/verifier"
)

func TestVerify(t *testing.T) {
	before := "package p\n// note\nfunc f(a int) int { var x = a + 1; return x }\n"
	tests := []struct {
		name, before, after string
		status              verifier.Status
	}{
		{"rename", before, strings.ReplaceAll(before, "x", "value"), verifier.Verified},
		{"capture", "package p; func f(a int) int { var x = 1; if a > 0 { var y = 2; return x + y }; return x }", "package p; func f(a int) int { var y = 1; if a > 0 { var y = 2; return y + y }; return y }", verifier.Rejected},
		{"partial", "package p; func f() int { var x = 1; var y = 2; return x + y }", "package p; func f() int { var z = 1; var x = 2; return x + z }", verifier.Rejected},
		{"mixed", before, strings.ReplaceAll(strings.ReplaceAll(before, "x", "z"), "+ 1", "+ 2"), verifier.Rejected},
		{"comment", before, strings.ReplaceAll(strings.ReplaceAll(before, "x", "z"), "note", "changed"), verifier.Rejected},
		{"string literal", `package p; func f() string { var x = "a"; return x }`, `package p; func f() string { var y = "b"; return y }`, verifier.Rejected},
		{"other binding captured", `package p; func f() int { var x = 1; if x > 0 { var y = 2; return x + y }; return x }`, `package p; func f() int { var y = 1; if y > 0 { var y = 2; return y + y }; return y }`, verifier.Rejected},
		{"merge conflict", before, "package p\n<<<<<<< HEAD\n=======\n>>>>>>> branch\n", verifier.Unsupported},
		{"nonlocal", before, strings.ReplaceAll(before, "f(", "g("), verifier.Rejected},
		{"invalid", before, "package p; func f( {", verifier.Unsupported},
		{"import", before, "package p; import \"fmt\"; func f() { fmt.Println() }", verifier.Unsupported},
		{"directive", before, "package p\n//go:noinline\nfunc f() int { var x = 1; return x }", verifier.Unsupported},
		{"fields", before, "package p; func f() int { var x = struct{ n int }{1}; return x.n }", verifier.Unsupported},
		{"generic", before, "package p; func f[T any](x T) T { return x }", verifier.Unsupported},
		{"named result", before, "package p; func f() (x int) { return }", verifier.Unsupported},
		{"type error", before, "package p; func f() int { var x = \"bad\"; return x }", verifier.Unsupported},
		{"no change", before, before, verifier.Rejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := verifier.Verify("input.go", []byte(tt.before), []byte(tt.after))
			require.Equal(t, tt.status, result.Status, result.Reason)
			if tt.status != verifier.Verified {
				require.NotEmpty(t, result.Reason)
				require.Nil(t, result.Evidence)
				return
			}
			require.NotNil(t, result.Evidence)
			require.Equal(t, "input.go", result.Evidence.File)
			require.Equal(t, "f", result.Evidence.Function)
			require.Equal(t, "x", result.Evidence.BeforeName)
			require.Equal(t, "value", result.Evidence.AfterName)
			require.NotEmpty(t, result.Evidence.LanguageVersion)
			require.NotEmpty(t, result.Evidence.AnalyzerVersion)
			require.NotEmpty(t, result.Evidence.CheckedConditions)
			require.NotEmpty(t, result.Evidence.Assumptions)
		})
	}
}
