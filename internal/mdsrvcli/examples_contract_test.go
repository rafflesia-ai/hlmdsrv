package mdsrvcli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMDSrvExamplesContract(t *testing.T) {
	root := mdsrvRepoRootForTest(t)
	for _, name := range []string{"mdsrv-demo.job.yaml", "mdsrv.job.yaml"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, "examples", name)
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
			out := runCLI(t, "explain", path, "--store", filepath.Join(t.TempDir(), "store"), "--json")
			var report map[string]any
			if err := json.Unmarshal([]byte(out), &report); err != nil {
				t.Fatalf("explain output is not JSON: %v\n%s", err, out)
			}
			if report["id"] == "" || report["plan"] == nil {
				t.Fatalf("unexpected explain report: %#v", report)
			}
		})
	}
}

func mdsrvRepoRootForTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root not found from %s: %v", file, err)
	}
	return root
}
