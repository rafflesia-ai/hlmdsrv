package mdsrvcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

type mdsrvJSONContractSnapshot struct {
	Contract string         `json:"contract"`
	Stable   map[string]any `json:"stable"`
	Shape    any            `json:"shape"`
}

func TestMDSrvJSONContractSnapshots(t *testing.T) {
	dir := t.TempDir()
	topology := filepath.Join(dir, "structure.gro")
	trajectory := filepath.Join(dir, "trajectory.xtc")
	if err := os.WriteFile(topology, []byte(gromacsFixtureGROForContract()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trajectory, []byte("placeholder trajectory for offline contract tests\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	jobPath := writeMDSrvContractJob(t, dir, topology, trajectory)
	reportPath := filepath.Join(dir, "run-report.json")
	batchPath := writeMDSrvContractBatch(t, dir)

	cases := []struct {
		name      string
		raw       string
		stable    map[string][]any
		transform func(any) any
	}{
		{
			name: "mdsrv-version",
			raw:  runCLI(t, "version", "--json"),
			stable: map[string][]any{
				"ok":               {"ok"},
				"service":          {"service"},
				"manifest_version": {"manifest_version"},
				"cli_version":      {"cli", "version"},
			},
			transform: projectMDSrvVersionContract,
		},
		{
			name: "mdsrv-capabilities-no-gromacs",
			raw:  runCLI(t, "capabilities", "--store", filepath.Join(dir, "capabilities-store"), "--gmx-command", "missing-contract-gmx", "--json"),
			stable: map[string][]any{
				"ok":                    {"ok"},
				"manifest_version":      {"manifest_version"},
				"gromacs_available":     {"gromacs", "available"},
				"gromacs_source":        {"gromacs", "source"},
				"chunk_encodings_count": {"features", "chunk_encodings", "len"},
			},
			transform: projectMDSrvCapabilitiesContract,
		},
		{
			name: "mdsrv-doctor-gromacs-check",
			raw:  runCLI(t, "doctor", "--gmx-command", "missing-contract-gmx", "--json"),
			stable: map[string][]any{
				"name":  {"name"},
				"ok":    {"ok"},
				"level": {"level"},
			},
			transform: projectMDSrvDoctorGromacsCheck,
		},
		{
			name: "mdsrv-self-test-core",
			raw:  runCLI(t, "self-test", "--out-dir", filepath.Join(dir, "self-test"), "--quickstart=false", "--json"),
			stable: map[string][]any{
				"ok":                {"ok"},
				"quickstart_status": {"quickstart_status"},
				"plan_steps":        {"plan", "steps", "len"},
			},
			transform: projectMDSrvSelfTestContract,
		},
		{
			name: "mdsrv-store-doctor",
			raw:  runCLI(t, "store", "doctor", "--store", filepath.Join(dir, "doctor-store"), "--init", "--json"),
			stable: map[string][]any{
				"ok":               {"ok"},
				"version":          {"version"},
				"expected_version": {"expected_version"},
				"checks_count":     {"checks", "len"},
			},
			transform: projectMDSrvStoreDoctorContract,
		},
		{
			name: "mdsrv-run-plan",
			raw:  runCLI(t, "run", jobPath, "--store", filepath.Join(dir, "plan-store"), "--plan", "--json"),
			stable: map[string][]any{
				"id":          {"id"},
				"steps_count": {"steps", "len"},
				"first_step":  {"steps", 0, "action"},
			},
		},
		{
			name: "mdsrv-run-report",
			raw:  runCLI(t, "run", jobPath, "--store", filepath.Join(dir, "run-store"), "--probe=false", "--index=false", "--force", "--report", reportPath, "--json"),
			stable: map[string][]any{
				"id":              {"id"},
				"artifacts_count": {"artifacts", "len"},
				"timings_count":   {"timings", "len"},
			},
		},
		{
			name: "mdsrv-batch-jsonl",
			raw:  firstJSONLine(runCLI(t, "batch", batchPath, "--store", filepath.Join(dir, "batch-store"), "--force", "--json")),
			stable: map[string][]any{
				"index": {"index"},
				"id":    {"id"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertMDSrvJSONContractSnapshot(t, tc.name, tc.raw, tc.stable, tc.transform)
		})
	}
}

func TestMDSrvHTTPJSONContractSnapshots(t *testing.T) {
	store, _, _ := makeHTTPFixtureStore(t)
	handler, _, err := (app{stdout: io.Discard, stderr: io.Discard}).serveHTTPHandler(store, &serveFlags{
		backend:        "gromacs",
		gmxCommand:     "missing-http-contract-gmx",
		workers:        1,
		maxQueue:       2,
		maxFrameRange:  2,
		jobTimeout:     2 * time.Second,
		jobTTL:         24 * time.Hour,
		requestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	submitted := mdsrvHTTPContractJSON(t, server, http.MethodPost, "/jobs", `{"type":"chunks","dataset_id":"run1","chunk_size":2,"encoding":"json"}`, "")
	jobID, _ := mdsrvContractPathValue(mdsrvHTTPContractBody(submitted), "id").(string)
	if jobID == "" {
		t.Fatalf("job submit did not return id: %s", submitted)
	}
	waitForMDSrvHTTPContractJob(t, server, jobID)

	readOnlyStore, _, _ := makeHTTPFixtureStore(t)
	readOnlyHandler, _, err := (app{stdout: io.Discard, stderr: io.Discard}).serveHTTPHandler(readOnlyStore, &serveFlags{
		readOnly:      true,
		maxFrameRange: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	readOnlyServer := httptest.NewServer(readOnlyHandler)
	defer readOnlyServer.Close()

	authStore, _, _ := makeHTTPFixtureStore(t)
	authHandler, _, err := (app{stdout: io.Discard, stderr: io.Discard}).serveHTTPHandler(authStore, &serveFlags{
		authToken:     "contract-secret",
		maxFrameRange: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	authServer := httptest.NewServer(authHandler)
	defer authServer.Close()

	cases := []struct {
		name   string
		raw    string
		stable map[string][]any
	}{
		{
			name: "mdsrv-http-health",
			raw:  mdsrvHTTPContractJSON(t, server, http.MethodGet, "/health", "", ""),
			stable: map[string][]any{
				"status": {"status"},
				"body":   {"body", "status"},
			},
		},
		{
			name: "mdsrv-http-capabilities",
			raw:  mdsrvHTTPContractJSON(t, server, http.MethodGet, "/capabilities", "", ""),
			stable: map[string][]any{
				"status":          {"status"},
				"job_queue":       {"body", "job_queue"},
				"workers":         {"body", "workers"},
				"max_frame_range": {"body", "max_frame_range"},
			},
		},
		{
			name: "mdsrv-http-datasets",
			raw:  mdsrvHTTPContractJSON(t, server, http.MethodGet, "/datasets", "", ""),
			stable: map[string][]any{
				"status": {"status"},
				"count":  {"body", "len"},
				"id":     {"body", 0, "id"},
			},
		},
		{
			name: "mdsrv-http-frames-count",
			raw:  mdsrvHTTPContractJSON(t, server, http.MethodGet, "/datasets/run1/frames/count", "", ""),
			stable: map[string][]any{
				"status":      {"status"},
				"frame_count": {"body", "frames"},
			},
		},
		{
			name: "mdsrv-http-jobs-list",
			raw:  mdsrvHTTPContractJSON(t, server, http.MethodGet, "/jobs", "", ""),
			stable: map[string][]any{
				"status": {"status"},
				"count":  {"body", "len"},
			},
		},
		{
			name: "mdsrv-http-job-events",
			raw:  mdsrvHTTPContractJSON(t, server, http.MethodGet, "/jobs/"+jobID+"/events?format=json", "", ""),
			stable: map[string][]any{
				"status":       {"status"},
				"events_count": {"body", "events", "len"},
			},
		},
		{
			name: "mdsrv-http-read-only-error",
			raw:  mdsrvHTTPContractJSON(t, readOnlyServer, http.MethodPost, "/datasets", `{"id":"blocked"}`, ""),
			stable: map[string][]any{
				"status": {"status"},
				"code":   {"body", "code"},
			},
		},
		{
			name: "mdsrv-http-auth-error",
			raw:  mdsrvHTTPContractJSON(t, authServer, http.MethodGet, "/datasets", "", ""),
			stable: map[string][]any{
				"status": {"status"},
				"code":   {"body", "code"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertMDSrvJSONContractSnapshot(t, tc.name, tc.raw, tc.stable, nil)
		})
	}
}

func writeMDSrvContractJob(t *testing.T, dir, topology, trajectory string) string {
	t.Helper()
	jobPath := filepath.Join(dir, "contract.mdsrv.yaml")
	data := fmt.Sprintf(`version: mdsrv.job/v1
metadata:
  id: contract-run
  name: Contract Run
inputs:
  topology:
    path: %q
    format: gro
  trajectories:
    - path: %q
      format: xtc
      time_unit: ps
      coordinate_unit: nm
`, filepath.ToSlash(topology), filepath.ToSlash(trajectory))
	if err := os.WriteFile(jobPath, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return jobPath
}

func writeMDSrvContractBatch(t *testing.T, dir string) string {
	t.Helper()
	rawDir := filepath.Join(dir, "raw")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "structure.gro"), []byte(gromacsFixtureGROForContract()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "trajectory.xtc"), []byte("placeholder trajectory for batch contract\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	batchPath := filepath.Join(dir, "jobs.jsonl")
	if err := os.WriteFile(batchPath, []byte(`{"id":"contract-batch","topology":"raw/structure.gro","trajectory":"raw/trajectory.xtc"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return batchPath
}

func gromacsFixtureGROForContract() string {
	return `Contract t= 0.0
    1
    1MOL     C1    1   0.100   0.100   0.100
   1.00000   1.00000   1.00000
`
}

func firstJSONLine(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return "{}"
}

func assertMDSrvJSONContractSnapshot(t *testing.T, name string, raw string, stablePaths map[string][]any, transform func(any) any) {
	t.Helper()
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("%s stdout is not JSON: %v\n%s", name, err, raw)
	}
	if transform != nil {
		value = transform(value)
	}
	stable := make(map[string]any, len(stablePaths))
	keys := make([]string, 0, len(stablePaths))
	for key := range stablePaths {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		stable[key] = mdsrvContractPathValue(value, stablePaths[key]...)
	}
	snapshot := mdsrvJSONContractSnapshot{
		Contract: name,
		Stable:   stable,
		Shape:    mdsrvContractShape(value),
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	path := filepath.Join("testdata", "contracts", name+".json")
	if os.Getenv("UPDATE_MDSRV_CONTRACT_SNAPSHOTS") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read contract snapshot %s: %v; set UPDATE_MDSRV_CONTRACT_SNAPSHOTS=1 to write it", path, err)
	}
	if !bytes.Equal(golden, data) {
		t.Fatalf("%s JSON contract changed; set UPDATE_MDSRV_CONTRACT_SNAPSHOTS=1 if this is intentional\n%s", name, mdsrvContractSnapshotDiff(golden, data))
	}
}

func projectMDSrvVersionContract(value any) any {
	root, _ := value.(map[string]any)
	cli, _ := root["cli"].(map[string]any)
	return map[string]any{
		"ok":               root["ok"],
		"service":          root["service"],
		"manifest_version": root["manifest_version"],
		"cli": map[string]any{
			"version":    cli["version"],
			"go_version": cli["go_version"],
			"goos":       cli["goos"],
			"goarch":     cli["goarch"],
		},
	}
}

func projectMDSrvCapabilitiesContract(value any) any {
	root, _ := value.(map[string]any)
	backends, _ := root["backends"].(map[string]any)
	gromacs, _ := backends["gromacs"].(map[string]any)
	return map[string]any{
		"ok":               root["ok"],
		"manifest_version": root["manifest_version"],
		"gromacs":          gromacs,
		"features":         root["features"],
	}
}

func projectMDSrvDoctorGromacsCheck(value any) any {
	root, _ := value.(map[string]any)
	checks, _ := root["checks"].([]any)
	for _, check := range checks {
		item, _ := check.(map[string]any)
		if item["name"] == "gromacs" {
			return item
		}
	}
	return map[string]any{"name": "gromacs", "ok": false, "missing": true}
}

func projectMDSrvSelfTestContract(value any) any {
	root, _ := value.(map[string]any)
	return map[string]any{
		"ok":                root["ok"],
		"quickstart_status": root["quickstart_status"],
		"checks":            root["checks"],
		"plan":              root["plan"],
	}
}

func projectMDSrvStoreDoctorContract(value any) any {
	root, _ := value.(map[string]any)
	return map[string]any{
		"ok":               root["ok"],
		"version":          root["version"],
		"expected_version": root["expected_version"],
		"checks":           root["checks"],
		"migrations":       root["migrations"],
	}
}

func mdsrvHTTPContractJSON(t *testing.T, server *httptest.Server, method, path, body, token string) string {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var decoded any
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		decoded = map[string]any{"decode_error": err.Error()}
	}
	payload := map[string]any{
		"status": resp.StatusCode,
		"body":   decoded,
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		t.Fatal(err)
	}
	return buffer.String()
}

func mdsrvHTTPContractBody(raw string) any {
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil
	}
	return payload["body"]
}

func waitForMDSrvHTTPContractJob(t *testing.T, server *httptest.Server, jobID string) {
	t.Helper()
	for attempt := 0; attempt < 50; attempt++ {
		raw := mdsrvHTTPContractJSON(t, server, http.MethodGet, "/jobs/"+jobID, "", "")
		body, _ := mdsrvHTTPContractBody(raw).(map[string]any)
		status, _ := body["status"].(string)
		switch status {
		case "succeeded", "failed", "canceled":
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach terminal status", jobID)
}

func mdsrvContractShape(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if mdsrvContractVolatileShapeKey(key) {
				continue
			}
			if key == "command" {
				if _, ok := child.([]any); ok {
					out[key] = []any{"<command>"}
					continue
				}
			}
			out[key] = mdsrvContractShape(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = mdsrvContractShape(child)
		}
		return out
	case json.Number:
		return "<number>"
	case string:
		return "<string>"
	case bool:
		return "<bool>"
	case nil:
		return nil
	default:
		return fmt.Sprintf("<%T>", typed)
	}
}

func mdsrvContractVolatileShapeKey(key string) bool {
	switch key {
	case "root", "store", "path", "manifest", "topology", "trajectory", "frames", "run_report", "executable", "sha256", "bytes", "total_duration_ms", "duration_ms", "started_at":
		return true
	default:
		return false
	}
}

func mdsrvContractPathValue(value any, path ...any) any {
	current := value
	for _, segment := range path {
		if keyword, ok := segment.(string); ok {
			switch keyword {
			case "len":
				switch typed := current.(type) {
				case []any:
					return len(typed)
				case map[string]any:
					return len(typed)
				case string:
					return len(typed)
				default:
					return 0
				}
			case "present":
				return current != nil
			}
		}
		switch typed := segment.(type) {
		case string:
			object, ok := current.(map[string]any)
			if !ok {
				return nil
			}
			current = object[typed]
		case int:
			array, ok := current.([]any)
			if !ok || typed < 0 || typed >= len(array) {
				return nil
			}
			current = array[typed]
		default:
			return nil
		}
	}
	return current
}

func mdsrvContractSnapshotDiff(want, got []byte) string {
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(string(got), "\n")
	var b strings.Builder
	max := len(wantLines)
	if len(gotLines) > max {
		max = len(gotLines)
	}
	for i := 0; i < max; i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			fmt.Fprintf(&b, "line %d\n- %s\n+ %s\n", i+1, w, g)
		}
	}
	return b.String()
}
