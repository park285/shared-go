package cipolicy_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWorkflowPolicyRegressionSuite(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	cmd := exec.CommandContext(t.Context(), "bash", "scripts/ci/check-workflow-secrets_test.sh")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("workflow policy regression suite failed: %v\n%s", err, output)
	}
}
