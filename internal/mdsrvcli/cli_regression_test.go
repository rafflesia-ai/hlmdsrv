package mdsrvcli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rafflesia-ai/hlmdsrv/internal/gromacs"
	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

func TestGromacsRealXTCIrregularTimestamps(t *testing.T) {
	gmx := gromacs.New(gromacs.Options{})
	if !gmx.Available() {
		t.Skip("GROMACS is not available")
	}
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "irregular.gro")
	xtcPath := filepath.Join(dir, "irregular.xtc")
	times := []float64{0, 0.5, 2.25, 7}
	if err := os.WriteFile(framesPath, []byte(irregularDemoGRO(times)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gmx.Convert(context.Background(), gromacs.ConvertOptions{Input: framesPath, Output: xtcPath}); err != nil {
		t.Fatal(err)
	}
	probe, err := gmx.Probe(context.Background(), xtcPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.FrameTimes) != len(times) {
		t.Fatalf("frame times were not preserved: %#v", probe)
	}
	for i, want := range times {
		if probe.FrameTimes[i] != want {
			t.Fatalf("frame time %d = %g, want %g", i, probe.FrameTimes[i], want)
		}
	}
}

func TestCLIRegressionGromacsFlow(t *testing.T) {
	if !gromacs.New(gromacs.Options{}).Available() {
		t.Skip("GROMACS is not available")
	}
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw")
	store := filepath.Join(dir, "store")

	runCLI(t, "demo", "gromacs", "--out", raw, "--frames", "6", "--json")
	ingestOut := runCLI(t,
		"ingest",
		filepath.Join(raw, "structure.gro"),
		filepath.Join(raw, "trajectory.xtc"),
		"--store", store,
		"--id", "run1",
		"--name", "Run 1",
		"--force",
		"--json",
	)
	var ingest struct {
		ID         string `json:"id"`
		FrameCount int    `json:"frame_count"`
		AtomCount  int    `json:"atom_count"`
	}
	decodeJSON(t, ingestOut, &ingest)
	if ingest.ID != "run1" || ingest.FrameCount != 6 || ingest.AtomCount != 3 {
		t.Fatalf("unexpected ingest report: %#v", ingest)
	}

	indexOut := runCLI(t, "index", "build", "run1", "--store", store, "--chunk-size", "2", "--json")
	var index mdsrv.FrameIndex
	decodeJSON(t, indexOut, &index)
	if index.FrameCount != 6 || len(index.Frames) != 6 || len(index.Chunks) != 3 {
		t.Fatalf("unexpected frame index: %#v", index)
	}
	chunkOut := runCLI(t, "index", "chunks", "run1", "--store", store, "--chunk-size", "2", "--encoding", "bin-zstd", "--force", "--json")
	decodeJSON(t, chunkOut, &index)
	if len(index.Chunks) != 3 || index.Chunks[0].Path == "" {
		t.Fatalf("unexpected materialized chunks: %#v", index.Chunks)
	}
	if index.Chunks[0].Encoding != mdsrv.FrameChunkEncodingBinaryZstd || !strings.HasSuffix(index.Chunks[0].Path, ".bin.zst") {
		t.Fatalf("unexpected chunk encoding/path: %#v", index.Chunks[0])
	}
	openedStore, err := mdsrv.OpenStore(store)
	if err != nil {
		t.Fatal(err)
	}
	decodedChunk, err := openedStore.LoadFrameChunk("run1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if decodedChunk.Encoding != mdsrv.FrameChunkEncodingBinaryZstd || len(decodedChunk.Frames) != 2 || len(decodedChunk.Frames[0].Coordinates) != 3 {
		t.Fatalf("unexpected decoded binary chunk: %#v", decodedChunk)
	}
	runCLI(t, "selection", "save", "run1", "--store", store, "--id", "first-two", "--expression", "1-2", "--json")

	frameOut := runCLI(t, "frames", "get", "run1", "3", "--store", store, "--backend", "auto", "--format", "json")
	var frame mdsrv.Frame
	decodeJSON(t, frameOut, &frame)
	if frame.Frame != 3 || len(frame.Coordinates) != 3 {
		t.Fatalf("unexpected frame: %#v", frame)
	}
	if backendDoctor, err := mdsrv.NewBackend(openedStore).Doctor(context.Background()); err == nil && (backendDoctor.MDTraj || backendDoctor.MDAnalysis) {
		subsetOut := runCLI(t, "frames", "get", "run1", "0", "--store", store, "--backend", "python", "--atom-subset", "all", "--format", "json")
		var subsetFrame mdsrv.Frame
		decodeJSON(t, subsetOut, &subsetFrame)
		if subsetFrame.Frame != 0 || len(subsetFrame.Coordinates) != 3 {
			t.Fatalf("unexpected Python atom-subset frame: %#v", subsetFrame)
		}
		if backendDoctor.MDTraj {
			assertPythonBackendGolden(t, store, "mdtraj", "mdtraj", "nm", 1)
		}
		if backendDoctor.MDAnalysis {
			assertPythonBackendGolden(t, store, "mdanalysis", "MDAnalysis", "angstrom", 10)
		}
	} else {
		t.Logf("skipping Python atom-subset frame assertion; backend unavailable")
	}

	tracePath := filepath.Join(dir, "distance.csv")
	resolvedSelection := strings.TrimSpace(runCLI(t, "selection", "resolve", "run1", "first-two", "--store", store, "--target", "python"))
	if resolvedSelection != "index 0 or index 1" {
		t.Fatalf("unexpected resolved selection %q", resolvedSelection)
	}
	analyzeOut := runCLI(t, "analyze", "distance", "run1", "--store", store, "--backend", "gromacs", "--a", "1", "--b", "2", "--out", tracePath, "--json")
	var analyzeReport map[string]any
	decodeJSON(t, analyzeOut, &analyzeReport)
	if analyzeReport["path"] != tracePath || analyzeReport["values"].(float64) == 0 {
		t.Fatalf("unexpected analyze report: %#v", analyzeReport)
	}
	if data, err := os.ReadFile(tracePath); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(data), "frame,time,value,unit") {
		t.Fatalf("unexpected trace output: %s", string(data))
	} else {
		assertDemoDistanceGolden(t, string(data), 6, "nm", 1)
	}

	slicePath := filepath.Join(dir, "slice.xtc")
	runCLI(t, "export", "run1", "--store", store, "--backend", "gromacs", "--frames", "1:4:2", "--out", slicePath, "--force", "--json")
	if _, err := os.Stat(slicePath); err != nil {
		t.Fatal(err)
	}
	convertedPath := filepath.Join(dir, "converted.xtc")
	runCLI(t, "gromacs", "convert", filepath.Join(raw, "frames.gro"), "--out", convertedPath, "--force", "--json")
	if _, err := os.Stat(convertedPath); err != nil {
		t.Fatal(err)
	}

	vizOut := runCLI(t, "visualize", "run1", "--store", store, "--frame", "2", "--include-selections", "--focus", "first-two", "--json")
	var vizReport struct {
		Scene      string   `json:"scene"`
		Snapshot   string   `json:"snapshot"`
		Components []string `json:"components"`
	}
	decodeJSON(t, vizOut, &vizReport)
	if vizReport.Snapshot == "" || len(vizReport.Components) < 2 {
		t.Fatalf("trajectory-aware visualization did not include snapshot/components: %#v", vizReport)
	}
	if _, err := os.Stat(filepath.Join(store, "visualization", "run1.mvsj")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store, "visualization", "run1-frame-2.gro")); err != nil {
		t.Fatal(err)
	}

	runCLI(t, "compat", "check", "--store", store, "--json")
	archive := filepath.Join(dir, "run1.mdsrvx")
	runCLI(t, "pack", "run1", "--store", store, "--out", archive, "--force", "--json")
	unpacked := filepath.Join(dir, "unpacked")
	runCLI(t, "unpack", archive, "--store", unpacked, "--force", "--json")
	inspectOut := runCLI(t, "dataset", "inspect", "run1", "--store", unpacked, "--backend", "gromacs", "--json")
	var inspect struct {
		FrameIndex *mdsrv.FrameIndex `json:"frame_index"`
	}
	decodeJSON(t, inspectOut, &inspect)
	if inspect.FrameIndex == nil || inspect.FrameIndex.FrameCount != 6 {
		t.Fatalf("packed frame index did not round-trip: %#v", inspect.FrameIndex)
	}
}

func TestHTTPServerParityRoutes(t *testing.T) {
	store, topology, trajectory := makeHTTPFixtureStore(t)
	mux := http.NewServeMux()
	registerHandlers(mux, store, "gromacs", "missing-gmx-for-route-test")
	server := httptest.NewServer(mux)
	defer server.Close()

	assertStatus(t, server, http.MethodGet, "/health", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/version", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/capabilities", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/schema/manifest", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/schema/batch", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/schema/openapi", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/datasets", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/datasets/run1", "", http.StatusOK)
	assertStatus(t, server, http.MethodPatch, "/datasets/run1", `{"name":"Updated Run 1"}`, http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/metadata", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/topology", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/trajectory", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/frames/count?backend=gromacs", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/frames/index", "", http.StatusOK)
	rawChunkResp := doRequest(t, server, http.MethodGet, "/datasets/run1/frames/chunks/0?format=raw", "")
	if rawChunkResp.StatusCode != http.StatusOK || !strings.Contains(rawChunkResp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("raw chunk response status/type = %d %q", rawChunkResp.StatusCode, rawChunkResp.Header.Get("Content-Type"))
	}
	rawChunkResp.Body.Close()
	assertStatus(t, server, http.MethodPost, "/datasets/run1/frames/index", `{"chunk_size":2}`, http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/frames/chunks", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/analyses", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/trajectory_index.json", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/session_index.json", "", http.StatusOK)

	run2Body := `{"id":"run2","topology":"` + escapeJSON(topology) + `","trajectory":"` + escapeJSON(trajectory) + `","force":true}`
	assertStatus(t, server, http.MethodPost, "/datasets", run2Body, http.StatusOK)
	assertStatus(t, server, http.MethodDelete, "/datasets/run2", "", http.StatusOK)

	assertStatus(t, server, http.MethodPost, "/datasets/run1/selections", `{"id":"first","expression":"1-2","kind":"atom-index"}`, http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/selections", "", http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/selections/first", "", http.StatusOK)
	assertStatus(t, server, http.MethodDelete, "/datasets/run1/selections/first", "", http.StatusOK)

	sessionPath := filepath.Join(filepath.Dir(topology), "session.molj")
	if err := os.WriteFile(sessionPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	sessionBody := `{"id":"run1-session","dataset_id":"run1","file":"` + escapeJSON(sessionPath) + `","force":true}`
	assertStatus(t, server, http.MethodPost, "/sessions", sessionBody, http.StatusOK)
	assertStatus(t, server, http.MethodGet, "/sessions", "", http.StatusOK)

	assertRouteExists(t, server, http.MethodGet, "/datasets/run1/frames/range?start=0&stop=0&backend=gromacs")
	assertRouteExists(t, server, http.MethodGet, "/datasets/run1/frames/0?format=json&backend=gromacs")
	assertRouteExists(t, server, http.MethodPost, "/datasets/run1/analyses?backend=gromacs", `{"type":"distance","selections":{"a":"1","b":"2"}}`)
	assertStatus(t, server, http.MethodPost, "/datasets/run1/rename", `{"new_id":"run-renamed"}`, http.StatusOK)
}

func TestHTTPServerOperationalControls(t *testing.T) {
	store, _, _ := makeHTTPFixtureStore(t)
	var logs bytes.Buffer
	mux := http.NewServeMux()
	registerHandlersWithOptions(mux, store, serverOptions{
		Backend:        "gromacs",
		GromacsCommand: "missing-gmx-for-route-test",
		ReadOnly:       true,
		MaxFrameRange:  1,
	})
	server := httptest.NewServer(requestLogMiddleware(mux, &logs))
	defer server.Close()

	assertStatus(t, server, http.MethodGet, "/health", "", http.StatusOK)
	if got := logs.String(); !strings.Contains(got, `"method":"GET"`) || !strings.Contains(got, `"path":"/health"`) {
		t.Fatalf("request log did not include method/path: %s", got)
	}
	assertStatus(t, server, http.MethodPost, "/datasets", `{"id":"blocked"}`, http.StatusForbidden)
	assertStatus(t, server, http.MethodPatch, "/datasets/run1", `{"name":"Blocked"}`, http.StatusForbidden)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/frames/range?start=0&stop=1&backend=gromacs", "", http.StatusBadRequest)

	limitStore, _, _ := makeHTTPFixtureStore(t)
	limitMux := http.NewServeMux()
	registerHandlersWithOptions(limitMux, limitStore, serverOptions{
		Limits:        mdsrv.ResourceLimits{MaxFrames: 1},
		MaxFrameRange: 256,
	})
	limitServer := httptest.NewServer(limitMux)
	defer limitServer.Close()
	assertStatus(t, limitServer, http.MethodGet, "/datasets/run1/frames/range?start=0&stop=0&backend=gromacs", "", http.StatusBadRequest)

	pathStore, topology, trajectory := makeHTTPFixtureStore(t)
	pathMux := http.NewServeMux()
	registerHandlersWithOptions(pathMux, pathStore, serverOptions{
		AllowPaths:    []string{filepath.Join(t.TempDir(), "allowed")},
		MaxFrameRange: 256,
	})
	pathServer := httptest.NewServer(pathMux)
	defer pathServer.Close()
	pathBody := `{"id":"outside","topology":"` + escapeJSON(topology) + `","trajectory":"` + escapeJSON(trajectory) + `","force":true}`
	assertStatus(t, pathServer, http.MethodPost, "/datasets", pathBody, http.StatusForbidden)

	hostStore, _, _ := makeHTTPFixtureStore(t)
	hostMux := http.NewServeMux()
	registerHandlersWithOptions(hostMux, hostStore, serverOptions{
		AllowHosts:    []string{"allowed.example"},
		MaxFrameRange: 256,
	})
	hostServer := httptest.NewServer(hostMux)
	defer hostServer.Close()
	hostBody := `{"id":"remote","topology_url":"https://blocked.example/structure.gro","trajectory_url":"https://blocked.example/traj.xtc","force":true}`
	assertStatus(t, hostServer, http.MethodPost, "/datasets", hostBody, http.StatusForbidden)

	authStore, _, _ := makeHTTPFixtureStore(t)
	authMux := http.NewServeMux()
	registerHandlersWithOptions(authMux, authStore, serverOptions{AuthToken: "secret", MaxFrameRange: 256})
	authServer := httptest.NewServer(requestIDMiddleware(authMiddleware(authMux, "secret")))
	defer authServer.Close()
	unauthorized := doRequest(t, authServer, http.MethodGet, "/datasets", "")
	var errorBody map[string]string
	if err := json.NewDecoder(unauthorized.Body).Decode(&errorBody); err != nil {
		t.Fatal(err)
	}
	_ = unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized || errorBody["code"] != "unauthorized" || errorBody["request_id"] == "" {
		t.Fatalf("unexpected auth error: status=%d body=%#v", unauthorized.StatusCode, errorBody)
	}
	req, err := http.NewRequest(http.MethodGet, authServer.URL+"/datasets", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := authServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorized request status = %d", resp.StatusCode)
	}
}

// TestServerAllowHostBlocksRedirectBypass proves the server's --allow-host
// policy is enforced across download redirects, not just on the initial URL.
// An allowed host (127.0.0.1, where httptest binds) redirects the download to a
// disallowed metadata IP; the ingest must be refused before that IP is reached.
func TestServerAllowHostBlocksRedirectBypass(t *testing.T) {
	decoy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer decoy.Close()

	store, _, _ := makeHTTPFixtureStore(t)
	mux := http.NewServeMux()
	registerHandlersWithOptions(mux, store, serverOptions{
		AllowHosts:    []string{"127.0.0.1"},
		MaxFrameRange: 256,
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	body := `{"id":"remote","topology_url":"` + decoy.URL + `/structure.gro","trajectory_url":"` + decoy.URL + `/traj.xtc","force":true}`
	resp := doRequest(t, server, http.MethodPost, "/datasets", body)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("redirect to a disallowed host was accepted")
	}
	var errorBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errorBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errorBody["error"], "not allowed") {
		t.Fatalf("expected host allowlist error, got: %#v", errorBody)
	}
}

// TestServerAnalysisOutputCannotEscapeStore proves the analysis output path is
// confined to the store: a "../" escape or an absolute path outside the store
// is rejected before any file is written.
func TestServerAnalysisOutputCannotEscapeStore(t *testing.T) {
	store, _, _ := makeHTTPFixtureStore(t)
	mux := http.NewServeMux()
	registerHandlersWithOptions(mux, store, serverOptions{Backend: "gromacs", MaxFrameRange: 256})
	server := httptest.NewServer(mux)
	defer server.Close()

	escapeTarget := filepath.Join(filepath.Dir(store.Root), "escaped-trace.csv")
	_ = os.Remove(escapeTarget)

	for _, output := range []string{"../escaped-trace.csv", escapeTarget} {
		body := `{"type":"rmsd","format":"csv","output":"` + escapeJSON(output) + `"}`
		resp := doRequest(t, server, http.MethodPost, "/datasets/run1/analyses", body)
		status := resp.StatusCode
		_ = resp.Body.Close()
		if status != http.StatusBadRequest {
			t.Fatalf("output %q: expected 400, got %d", output, status)
		}
	}
	if _, err := os.Stat(escapeTarget); !os.IsNotExist(err) {
		t.Fatalf("escaped file was written outside the store: err=%v", err)
	}
}

// TestResolveTraceOutputWithinStore unit-tests the confinement helper directly.
func TestResolveTraceOutputWithinStore(t *testing.T) {
	store, err := mdsrv.OpenStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../escape.csv", "../../etc/passwd", "/etc/passwd"} {
		if _, err := resolveTraceOutputWithinStore(store, bad, "run1", "rmsd"); err == nil {
			t.Fatalf("expected %q to be rejected", bad)
		}
	}
	// A relative path inside the store and the empty (default) path both resolve.
	for _, ok := range []string{"traces/run1.csv", ""} {
		resolved, err := resolveTraceOutputWithinStore(store, ok, "run1", "rmsd")
		if err != nil {
			t.Fatalf("expected %q to resolve, got %v", ok, err)
		}
		if rel, inside := storeRelativePathIfInside(store.Root, resolved); !inside {
			t.Fatalf("resolved path %q is outside the store (rel=%q)", resolved, rel)
		}
	}
}

// TestFramesCountUsesCachedManifestWithoutBackend proves `frames count` returns
// the cached manifest frame count without spawning a backend probe when no
// --backend is given — a bogus gmx command must not cause failure.
func TestFramesCountUsesCachedManifestWithoutBackend(t *testing.T) {
	dir := t.TempDir()
	topology := filepath.Join(dir, "structure.gro")
	trajectory := filepath.Join(dir, "traj.xtc")
	if err := os.WriteFile(topology, []byte("topology"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trajectory, []byte("trajectory"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := mdsrv.OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Ingest(mdsrv.IngestOptions{ID: "run1", Topology: topology, Trajectory: trajectory})
	if err != nil {
		t.Fatal(err)
	}
	m.Inputs.Trajectories[0].FrameCount = 42
	m.Inputs.Trajectories[0].AtomCount = 7
	if err := mdsrv.WriteManifestFile(store.ManifestPath("run1"), m); err != nil {
		t.Fatal(err)
	}

	out := runCLI(t, "frames", "count", "run1", "--store", store.Root, "--gmx-command", "definitely-not-a-real-gmx", "--json")
	var report map[string]any
	decodeJSON(t, out, &report)
	if report["frame_count"] != float64(42) {
		t.Fatalf("expected cached frame_count 42 without probing, got %v", report["frame_count"])
	}
}

func TestHTTPServerJobQueueRoutes(t *testing.T) {
	store, _, _ := makeHTTPFixtureStore(t)
	mux := http.NewServeMux()
	registerHandlersWithOptions(mux, store, serverOptions{
		Backend:        "gromacs",
		GromacsCommand: "missing-gmx-for-job-queue-test",
		MaxFrameRange:  256,
		Workers:        1,
		MaxQueue:       2,
		JobTimeout:     2 * time.Second,
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	assertStatus(t, server, http.MethodGet, "/jobs", "", http.StatusOK)
	resp := doRequest(t, server, http.MethodPost, "/jobs", `{"type":"chunks","dataset_id":"run1","chunk_size":4,"encoding":"json"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /jobs status=%d", resp.StatusCode)
	}
	var submitted serverJobStatus
	if err := json.NewDecoder(resp.Body).Decode(&submitted); err != nil {
		t.Fatal(err)
	}
	if submitted.ID == "" || submitted.Status != "queued" {
		t.Fatalf("unexpected submitted job: %#v", submitted)
	}
	var status serverJobStatus
	for attempt := 0; attempt < 50; attempt++ {
		jobResp := doRequest(t, server, http.MethodGet, "/jobs/"+submitted.ID, "")
		if jobResp.StatusCode != http.StatusOK {
			t.Fatalf("GET /jobs/%s status=%d", submitted.ID, jobResp.StatusCode)
		}
		if err := json.NewDecoder(jobResp.Body).Decode(&status); err != nil {
			t.Fatal(err)
		}
		jobResp.Body.Close()
		if status.Status == "succeeded" || status.Status == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status.Status != "succeeded" || status.Result["chunk_count"] == nil {
		t.Fatalf("unexpected job status: %#v", status)
	}
	statsResp := doRequest(t, server, http.MethodGet, "/jobs/stats", "")
	if statsResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /jobs/stats status=%d", statsResp.StatusCode)
	}
	var stats serverJobStats
	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	statsResp.Body.Close()
	if stats.Total == 0 || stats.Counts["succeeded"] == 0 {
		t.Fatalf("unexpected job stats: %#v", stats)
	}
	metricsResp := doRequest(t, server, http.MethodGet, "/jobs/metrics", "")
	metricsRaw, err := io.ReadAll(metricsResp.Body)
	metricsResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if metricsResp.StatusCode != http.StatusOK || !strings.Contains(string(metricsRaw), "mdsrv_jobs_by_status") {
		t.Fatalf("unexpected metrics status=%d body=%s", metricsResp.StatusCode, string(metricsRaw))
	}
	logResp := doRequest(t, server, http.MethodGet, "/jobs/"+submitted.ID+"/logs", "")
	logRaw, err := io.ReadAll(logResp.Body)
	logResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if logResp.StatusCode != http.StatusOK || !strings.Contains(string(logRaw), "submitted") || !strings.Contains(string(logRaw), "succeeded") {
		t.Fatalf("unexpected job log status=%d body=%s", logResp.StatusCode, string(logRaw))
	}
	eventsResp := doRequest(t, server, http.MethodGet, "/jobs/"+submitted.ID+"/events?format=json", "")
	if eventsResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /jobs/%s/events status=%d", submitted.ID, eventsResp.StatusCode)
	}
	var eventsPayload struct {
		Events []serverJobEvent `json:"events"`
	}
	if err := json.NewDecoder(eventsResp.Body).Decode(&eventsPayload); err != nil {
		t.Fatal(err)
	}
	eventsResp.Body.Close()
	if len(eventsPayload.Events) == 0 || eventsPayload.Events[len(eventsPayload.Events)-1].Type != jobEventSucceeded || eventsPayload.Events[len(eventsPayload.Events)-1].Version != jobEventVersion {
		t.Fatalf("unexpected job events: %#v", eventsPayload.Events)
	}
	retryResp := doRequest(t, server, http.MethodPost, "/jobs/"+submitted.ID+"/retry", `{}`)
	if retryResp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /jobs/%s/retry status=%d", submitted.ID, retryResp.StatusCode)
	}
	var retried serverJobStatus
	if err := json.NewDecoder(retryResp.Body).Decode(&retried); err != nil {
		t.Fatal(err)
	}
	retryResp.Body.Close()
	if retried.ID == "" || retried.ID == submitted.ID {
		t.Fatalf("unexpected retried job: %#v", retried)
	}
	cancelResp := doRequest(t, server, http.MethodPost, "/jobs/"+submitted.ID+"/cancel", `{}`)
	if cancelResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /jobs/%s/cancel status=%d", submitted.ID, cancelResp.StatusCode)
	}
	cancelResp.Body.Close()

	reloadedMux := http.NewServeMux()
	registerHandlersWithOptions(reloadedMux, store, serverOptions{Workers: 1, MaxQueue: 1, MaxFrameRange: 256})
	reloadedServer := httptest.NewServer(reloadedMux)
	defer reloadedServer.Close()
	reloadedResp := doRequest(t, reloadedServer, http.MethodGet, "/jobs/"+submitted.ID, "")
	if reloadedResp.StatusCode != http.StatusOK {
		t.Fatalf("persisted GET /jobs/%s status=%d", submitted.ID, reloadedResp.StatusCode)
	}
	reloadedResp.Body.Close()
	prunedHandler, _, err := (app{stdout: io.Discard, stderr: io.Discard}).serveHTTPHandler(store, &serveFlags{
		workers:         1,
		maxQueue:        1,
		maxFrameRange:   256,
		jobPruneOnStart: true,
		jobTTL:          0,
	})
	if err != nil {
		t.Fatal(err)
	}
	prunedServer := httptest.NewServer(prunedHandler)
	defer prunedServer.Close()
	prunedResp := doRequest(t, prunedServer, http.MethodGet, "/jobs/"+submitted.ID, "")
	if prunedResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected startup prune to remove job, status=%d", prunedResp.StatusCode)
	}
	prunedResp.Body.Close()

	readOnlyMux := http.NewServeMux()
	registerHandlersWithOptions(readOnlyMux, store, serverOptions{ReadOnly: true, Workers: 1, MaxQueue: 1, MaxFrameRange: 256})
	readOnlyServer := httptest.NewServer(readOnlyMux)
	defer readOnlyServer.Close()
	assertStatus(t, readOnlyServer, http.MethodPost, "/jobs", `{"type":"chunks","dataset_id":"run1"}`, http.StatusForbidden)
}

func TestJobsCommandServerRoundTrip(t *testing.T) {
	store, _, _ := makeHTTPFixtureStore(t)
	mux := http.NewServeMux()
	registerHandlersWithOptions(mux, store, serverOptions{
		Backend:        "gromacs",
		GromacsCommand: "missing-gmx-for-job-cli-test",
		MaxFrameRange:  256,
		Workers:        1,
		MaxQueue:       2,
		JobTimeout:     2 * time.Second,
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	submitOut := runCLI(t, "jobs", "--server", server.URL, "submit", "--type", "chunks", "--dataset", "run1", "--chunk-size", "4", "--encoding", "json", "--wait", "--json")
	var submitted serverJobStatus
	decodeJSON(t, submitOut, &submitted)
	if submitted.ID == "" || submitted.Status != "succeeded" {
		t.Fatalf("unexpected submitted job: %#v", submitted)
	}
	statusOut := runCLI(t, "jobs", "--server", server.URL, "status", submitted.ID, "--json")
	var status serverJobStatus
	decodeJSON(t, statusOut, &status)
	if status.ID != submitted.ID || status.Status != "succeeded" {
		t.Fatalf("unexpected status: %#v", status)
	}
	logOut := runCLI(t, "jobs", "--server", server.URL, "logs", submitted.ID)
	if !strings.Contains(logOut, "submitted") || !strings.Contains(logOut, "succeeded") {
		t.Fatalf("unexpected logs:\n%s", logOut)
	}
	eventsOut := runCLI(t, "jobs", "--server", server.URL, "events", submitted.ID, "--json")
	var events struct {
		Events []serverJobEvent `json:"events"`
	}
	decodeJSON(t, eventsOut, &events)
	if len(events.Events) == 0 {
		t.Fatalf("unexpected events: %#v", events)
	}
	retryOut := runCLI(t, "jobs", "--server", server.URL, "retry", submitted.ID, "--wait", "--json")
	var retried serverJobStatus
	decodeJSON(t, retryOut, &retried)
	if retried.ID == "" || retried.ID == submitted.ID || retried.Status != "succeeded" {
		t.Fatalf("unexpected retry status: %#v", retried)
	}
	cancelOut := runCLI(t, "jobs", "--server", server.URL, "cancel", submitted.ID, "--json")
	var canceled serverJobStatus
	decodeJSON(t, cancelOut, &canceled)
	if canceled.ID != submitted.ID {
		t.Fatalf("unexpected cancel status: %#v", canceled)
	}
	listOut := runCLI(t, "jobs", "--server", server.URL, "list", "--json")
	var statuses []serverJobStatus
	decodeJSON(t, listOut, &statuses)
	if len(statuses) == 0 {
		t.Fatalf("jobs list returned no jobs")
	}
	statsOut := runCLI(t, "jobs", "--server", server.URL, "stats", "--json")
	var stats serverJobStats
	decodeJSON(t, statsOut, &stats)
	if stats.Total == 0 || stats.Counts["succeeded"] == 0 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	metricsOut := runCLI(t, "jobs", "--server", server.URL, "stats")
	if !strings.Contains(metricsOut, "workers") {
		t.Fatalf("unexpected human stats output: %s", metricsOut)
	}
	pruneDryRunOut := runCLI(t, "jobs", "prune", "--store", store.Root, "--ttl", "0", "--dry-run", "--json")
	var dryRun jobPruneReport
	decodeJSON(t, pruneDryRunOut, &dryRun)
	if len(dryRun.WouldRemove) == 0 {
		t.Fatalf("expected dry-run prune candidate: %#v", dryRun)
	}
	pruneOut := runCLI(t, "jobs", "prune", "--store", store.Root, "--ttl", "0", "--json")
	var prune jobPruneReport
	decodeJSON(t, pruneOut, &prune)
	if len(prune.Removed) == 0 {
		t.Fatalf("expected pruned job: %#v", prune)
	}
}

func TestBenchCommandReportsCodecs(t *testing.T) {
	out := runCLI(t, "bench", "--frames", "4", "--atoms", "3", "--iterations", "1", "--json")
	var report benchReport
	decodeJSON(t, out, &report)
	if !report.OK || len(report.Results) != 3 {
		t.Fatalf("unexpected benchmark report: %#v", report)
	}
	for _, result := range report.Results {
		if result.Bytes == 0 || result.Encoding == "" {
			t.Fatalf("unexpected benchmark result: %#v", result)
		}
	}
}

func TestServeCommandProcessEndToEnd(t *testing.T) {
	store, _, _ := makeHTTPFixtureStore(t)
	port := freeTCPPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	a := app{stdout: &stdout, stderr: &stderr}
	done := make(chan error, 1)
	go func() {
		done <- a.runServe(ctx, store, &serveFlags{
			store:         store.Root,
			host:          "127.0.0.1",
			port:          port,
			backend:       "gromacs",
			gmxCommand:    "missing-gmx-for-serve-process-test",
			readOnly:      true,
			logRequests:   true,
			maxFrameRange: 256,
		})
	}()
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForHTTPStatus(t, baseURL+"/health", http.StatusOK)
	client := &http.Client{Timeout: 2 * time.Second}
	for _, path := range []string{
		"/datasets",
		"/schema/openapi",
		"/datasets/run1/frames/chunks/0?format=raw",
		"/trajectory_index.json",
	} {
		resp, err := client.Get(baseURL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d", path, resp.StatusCode)
		}
	}
	resp, err := client.Post(baseURL+"/datasets", "application/json", strings.NewReader(`{"id":"blocked"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only POST status=%d", resp.StatusCode)
	}
	if !strings.Contains(stderr.String(), `"path":"/health"`) {
		t.Fatalf("serve command did not log requests: %s", stderr.String())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned error: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not shut down after context cancellation")
	}
}

func TestRunJobManifestEndToEnd(t *testing.T) {
	dir := t.TempDir()
	topology := filepath.Join(dir, "structure.gro")
	trajectory := filepath.Join(dir, "traj.xtc")
	if err := os.WriteFile(topology, []byte("topology"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trajectory, []byte("trajectory"), 0o644); err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(dir, "job.yaml")
	archivePath := filepath.Join(dir, "run1.mdsrvx")
	job := `
version: mdsrv.job/v1
metadata:
  id: run1
  name: Run 1
inputs:
  topology:
    path: structure.gro
    format: gro
  trajectories:
    - path: traj.xtc
      format: xtc
selections:
  - id: first-two
    expression: atom:1-2
    kind: atom-index
outputs:
  - type: mdsrvx
    path: run1.mdsrvx
`
	if err := os.WriteFile(jobPath, []byte(job), 0o644); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(dir, "store")
	reportPath := filepath.Join(dir, "run-report.json")
	out := runCLI(t, "run", jobPath, "--store", store, "--probe=false", "--index=false", "--force", "--report", reportPath, "--json")
	var rawReport map[string]any
	decodeJSON(t, out, &rawReport)
	assertJSONKeys(t, rawReport, "archives", "artifacts", "id", "manifest", "store", "timings", "total_duration_ms")
	var report runReport
	decodeJSON(t, out, &report)
	if report.ID != "run1" || len(report.Archives) != 1 {
		t.Fatalf("unexpected run report: %#v", report)
	}
	var fileReport runReport
	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	decodeJSON(t, string(reportData), &fileReport)
	if fileReport.ID != report.ID || len(fileReport.Artifacts) == 0 || len(fileReport.Timings) == 0 {
		t.Fatalf("durable report missing artifacts/timings: %#v", fileReport)
	}
	assertNoDuplicateRunArtifacts(t, fileReport.Artifacts)
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatal(err)
	}
	loaded, err := mdsrv.LoadManifestFile(filepath.Join(store, "datasets", "run1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Selections) != 1 || loaded.Selections[0].Expression != "atom:1-2" {
		t.Fatalf("selection was not saved: %#v", loaded.Selections)
	}
}

func TestRunPlanJSONContract(t *testing.T) {
	dir := t.TempDir()
	jobPath := filepath.Join(dir, "job.yaml")
	job := `
version: mdsrv.job/v1
metadata:
  id: run1
inputs:
  topology:
    path: structure.gro
    format: gro
  trajectories:
    - path: traj.xtc
      format: xtc
streaming:
  chunk_size_frames: 2
  materialize_chunks: true
analyses:
  - id: d12
    type: distance
    selections:
      a: "1"
      b: "2"
    output: traces/d12.csv
visualization:
  mvs:
    scene: visualization/run1.mvsj
outputs:
  - type: mdsrvx
    path: run1.mdsrvx
`
	if err := os.WriteFile(jobPath, []byte(job), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runCLI(t, "run", jobPath, "--store", filepath.Join(dir, "store"), "--plan", "--json")
	var plan runPlanReport
	decodeJSON(t, out, &plan)
	if plan.ID != "run1" || len(plan.Steps) != 6 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	wantActions := []string{"ingest", "probe", "chunks", "visualize", "analysis", "pack"}
	for i, action := range wantActions {
		if plan.Steps[i].Action != action {
			t.Fatalf("step %d action = %q, want %q; plan=%#v", i, plan.Steps[i].Action, action, plan)
		}
	}
}

func TestServeSmokeIncludesJobStatsWhenWorkersEnabled(t *testing.T) {
	store, _, _ := makeHTTPFixtureStore(t)
	out := runCLI(t, "serve", "smoke", "--store", store.Root, "--workers", "1", "--json")
	var report serveSmokeReport
	decodeJSON(t, out, &report)
	if !report.OK {
		t.Fatalf("serve smoke failed: %#v", report)
	}
	found := false
	foundMetrics := false
	foundJobMetrics := false
	for _, check := range report.Checks {
		if check.Path == "/jobs/stats" && check.OK {
			found = true
		}
		if check.Path == "/metrics" && check.OK {
			foundMetrics = true
		}
		if check.Path == "/jobs/metrics" && check.OK {
			foundJobMetrics = true
		}
	}
	if !found || !foundMetrics || !foundJobMetrics {
		t.Fatalf("serve smoke did not check /jobs/stats: %#v", report.Checks)
	}
}

func TestStoreDoctorCommand(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	out := runCLI(t, "store", "doctor", "--store", dir, "--init", "--json")
	var report mdsrv.StoreDoctorReport
	decodeJSON(t, out, &report)
	if !report.OK || report.Version != mdsrv.StoreVersion || report.Metadata == "" {
		t.Fatalf("unexpected store doctor report: %#v", report)
	}
	strictOut := runCLI(t, "store", "doctor", "--store", dir, "--strict", "--json")
	var strictReport mdsrv.StoreDoctorReport
	decodeJSON(t, strictOut, &strictReport)
	if !strictReport.OK {
		t.Fatalf("strict store doctor failed: %#v", strictReport)
	}
}

func TestRunJobManifestGromacsEndToEnd(t *testing.T) {
	if !gromacs.New(gromacs.Options{}).Available() {
		t.Skip("GROMACS is not available")
	}
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw")
	store := filepath.Join(dir, "store")
	runCLI(t, "demo", "gromacs", "--out", raw, "--frames", "4", "--json")
	sessionPath := filepath.Join(dir, "session.molj")
	if err := os.WriteFile(sessionPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(dir, "job.yaml")
	job := `
version: mdsrv.job/v1
metadata:
  id: run1
  name: Run 1
inputs:
  topology:
    path: raw/structure.gro
    format: gro
  trajectories:
    - path: raw/trajectory.xtc
      format: xtc
      time_unit: ps
      coordinate_unit: nm
streaming:
  chunk_size_frames: 2
  materialize_chunks: true
selections:
  - id: first-two
    expression: 1-2
    kind: atom-index
analyses:
  - id: d12
    type: distance
    selections:
      a: "1"
      b: "2"
    output: traces/d12.csv
visualization:
  mvs:
    scene: visualization/run1.mvsj
  molstar:
    state: session.molj
outputs:
  - type: mdsrvx
    path: run1.mdsrvx
`
	if err := os.WriteFile(jobPath, []byte(job), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runCLI(t, "run", jobPath, "--store", store, "--backend", "gromacs", "--force", "--json")
	var report runReport
	decodeJSON(t, out, &report)
	if report.ID != "run1" || !report.Probed || report.Index == "" {
		t.Fatalf("unexpected run report: %#v", report)
	}
	if len(report.Chunks) != 2 || len(report.Analyses) != 1 || report.Visualization == "" || len(report.Sessions) != 1 || len(report.Archives) != 1 {
		t.Fatalf("run report missing artifacts: %#v", report)
	}
	for _, path := range []string{
		filepath.Join(store, report.Index),
		filepath.Join(store, report.Chunks[0]),
		filepath.Join(store, report.Visualization),
		filepath.Join(store, report.Analyses[0]),
		filepath.Join(store, report.Sessions[0]),
		filepath.Join(dir, "run1.mdsrvx"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected artifact %s: %v", path, err)
		}
	}
	loaded, err := mdsrv.LoadManifestFile(filepath.Join(store, "datasets", "run1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Streaming.Cache == "" || loaded.Visualization.MVS.Scene == "" || len(loaded.Visualization.Sessions) != 1 {
		t.Fatalf("manifest was not updated with run artifacts: %#v", loaded)
	}
}

func TestQuickstartEndToEnd(t *testing.T) {
	if !gromacs.New(gromacs.Options{}).Available() {
		t.Skip("GROMACS is not available")
	}
	dir := filepath.Join(t.TempDir(), "quickstart")
	out := runCLI(t, "quickstart", "--out", dir, "--id", "qs", "--frames", "4", "--json")
	var report quickstartReport
	decodeJSON(t, out, &report)
	if report.ID != "qs" || report.Job == "" || report.Store == "" || report.Static == "" || report.RunReport == "" {
		t.Fatalf("quickstart report missing core fields: %#v", report)
	}
	if report.Run.ID != "qs" || report.Run.Index == "" || len(report.Run.Chunks) == 0 || len(report.Run.Analyses) != 1 || report.Run.Visualization == "" {
		t.Fatalf("quickstart run did not create expected artifacts: %#v", report.Run)
	}
	if report.Publish.Verification == nil || !report.Publish.Verification.OK {
		t.Fatalf("quickstart static publish was not verified: %#v", report.Publish.Verification)
	}
	if !report.ServeSmoke.OK || len(report.ServeSmoke.Checks) == 0 {
		t.Fatalf("quickstart did not smoke-test the server surface: %#v", report.ServeSmoke)
	}
	for _, path := range []string{report.Job, report.RunReport, report.Archive, filepath.Join(report.Store, "datasets", "qs.yaml")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("quickstart artifact %s: %v", path, err)
		}
	}
	serveCommand := "hlmdsrv serve --store " + shellArg(report.Store) + " --read-only"
	if len(report.NextCommands) == 0 || report.NextCommands[0] != serveCommand {
		t.Fatalf("quickstart next commands are not actionable: %#v", report.NextCommands)
	}
}

func TestSelfTestCore(t *testing.T) {
	dir := t.TempDir()
	out := runCLI(t, "self-test", "--out", dir, "--quickstart=false", "--json")
	var report selfTestReport
	decodeJSON(t, out, &report)
	if !report.OK || report.Job == "" || report.Plan.ID != "self-test-plan" || len(report.Plan.Steps) == 0 {
		t.Fatalf("unexpected self-test report: %#v", report)
	}
	if report.QuickstartStatus != "disabled" {
		t.Fatalf("core self-test quickstart status = %q", report.QuickstartStatus)
	}
	if report.Quickstart != nil {
		t.Fatalf("core self-test should not run quickstart: %#v", report.Quickstart)
	}
	for _, path := range []string{report.Job, filepath.Join(report.Root, "store", "trajectory_index.json")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("self-test artifact %s: %v", path, err)
		}
	}
}

func TestHeadlessProductPolishCommands(t *testing.T) {
	dir := t.TempDir()
	store, topology, trajectory := makeHTTPFixtureStore(t)

	jobPath := filepath.Join(dir, "job.yaml")
	runCLI(t, "init", "job", "--id", "smooth", "--topology", topology, "--trajectory", trajectory, "--out", jobPath)
	explainOut := runCLI(t, "explain", jobPath, "--store", filepath.Join(dir, "store"), "--json")
	var explainJSON map[string]any
	decodeJSON(t, explainOut, &explainJSON)
	assertJSONHasKeys(t, explainJSON, "id", "version", "job", "job_root", "store", "backend", "inputs", "plan")
	var explanation explainReport
	decodeJSON(t, explainOut, &explanation)
	if explanation.ID != "smooth" || explanation.Inputs.Topology.Resolved == "" || len(explanation.Plan) == 0 {
		t.Fatalf("unexpected explain report: %#v", explanation)
	}

	completionOut := runCLI(t, "completion", "bash")
	if !strings.Contains(completionOut, "hlmdsrv") || !strings.Contains(completionOut, "complete") {
		t.Fatalf("bash completion output is incomplete:\n%s", completionOut)
	}
	versionOut := runCLI(t, "version", "--json")
	var version map[string]any
	decodeJSON(t, versionOut, &version)
	if version["service"] != "hlmdsrv" || version["manifest_version"] != mdsrv.ManifestVersion {
		t.Fatalf("unexpected version report: %#v", version)
	}
	capabilitiesOut := runCLI(t, "capabilities", "--store", filepath.Join(dir, "capabilities-store"), "--json")
	var capabilities map[string]any
	decodeJSON(t, capabilitiesOut, &capabilities)
	if capabilities["service"] != "hlmdsrv" || capabilities["features"] == nil {
		t.Fatalf("unexpected capabilities report: %#v", capabilities)
	}
	missingCapabilitiesOut := runCLI(t, "capabilities", "--store", filepath.Join(dir, "missing-gmx-capabilities-store"), "--gmx-command", "missing-capabilities-gmx", "--json")
	var missingCapabilities struct {
		Backends struct {
			Gromacs struct {
				Available bool   `json:"available"`
				Command   string `json:"command"`
				Source    string `json:"source"`
				Hint      string `json:"hint"`
			} `json:"gromacs"`
		} `json:"backends"`
	}
	decodeJSON(t, missingCapabilitiesOut, &missingCapabilities)
	if missingCapabilities.Backends.Gromacs.Available || missingCapabilities.Backends.Gromacs.Command != "missing-capabilities-gmx" || missingCapabilities.Backends.Gromacs.Source != "option" || missingCapabilities.Backends.Gromacs.Hint == "" {
		t.Fatalf("missing GROMACS capabilities did not include actionable diagnostics: %#v", missingCapabilities.Backends.Gromacs)
	}
	docsDir := filepath.Join(dir, "docs")
	runCLI(t, "docs", "--out", docsDir)
	if _, err := os.Stat(filepath.Join(docsDir, "hlmdsrv.md")); err != nil {
		t.Fatalf("docs command did not write root doc: %v", err)
	}

	smokeOut := runCLI(t, "serve", "smoke", "--store", store.Root, "--read-only")
	var smokeJSON map[string]any
	decodeJSON(t, smokeOut, &smokeJSON)
	assertJSONHasKeys(t, smokeJSON, "store", "checks", "ok", "dataset")
	var smoke serveSmokeReport
	decodeJSON(t, smokeOut, &smoke)
	if !smoke.OK || smoke.Dataset != "run1" || len(smoke.Checks) == 0 {
		t.Fatalf("unexpected serve smoke report: %#v", smoke)
	}

	stdout, _, err := runCLIError("doctor", "--strict", "--gmx-command", "missing-doctor-gmx", "--json")
	if err == nil || ErrorCode(err) != string(codeValidationFailed) {
		t.Fatalf("doctor strict missing gromacs error = %v code=%s", err, ErrorCode(err))
	}
	var doctor doctorReport
	decodeJSON(t, stdout, &doctor)
	var rawDoctor map[string]any
	decodeJSON(t, stdout, &rawDoctor)
	assertJSONHasKeys(t, rawDoctor, "ok", "checks")
	rawChecks, ok := rawDoctor["checks"].([]any)
	if !ok || len(rawChecks) == 0 {
		t.Fatal("doctor strict returned no checks")
	}
	firstCheck, ok := rawChecks[0].(map[string]any)
	if !ok {
		t.Fatalf("doctor check is not an object: %#v", rawChecks[0])
	}
	assertJSONHasKeys(t, firstCheck, "name", "level", "ok")
	var sawGromacs bool
	for _, check := range doctor.Checks {
		if check.Name == "gromacs" && !check.OK {
			sawGromacs = true
		}
	}
	if !sawGromacs {
		t.Fatalf("doctor strict output did not include failed gromacs check: %#v", doctor.Checks)
	}

	manifest, err := store.LoadDataset("run1")
	if err != nil {
		t.Fatal(err)
	}
	manifest.Visualization.MVS.Scene = "visualization/missing.mvsj"
	if err := mdsrv.WriteManifestFile(store.ManifestPath("run1"), manifest); err != nil {
		t.Fatal(err)
	}
	stdout, _, err = runCLIError("validate", "run1", "--store", store.Root, "--strict", "--json")
	if err == nil || ErrorCode(err) != string(codeValidationFailed) {
		t.Fatalf("validate strict missing artifact error = %v code=%s", err, ErrorCode(err))
	}
	var validation struct {
		Issues []validationIssue `json:"issues"`
	}
	var validationJSON map[string]any
	decodeJSON(t, stdout, &validationJSON)
	assertJSONHasKeys(t, validationJSON, "id", "files", "ok", "issues")
	decodeJSON(t, stdout, &validation)
	var sawMissingMVS bool
	for _, issue := range validation.Issues {
		if issue.Kind == "mvs_scene" && issue.Severity == "error" {
			sawMissingMVS = true
		}
	}
	if !sawMissingMVS {
		t.Fatalf("validate strict did not report missing MVS scene: %#v", validation.Issues)
	}
}

func TestDebugBundleCommand(t *testing.T) {
	store, _, _ := makeHTTPFixtureStore(t)
	jobDir := filepath.Join(store.Root, mdsrv.JobsDir, "job_debug")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "status.json"), []byte(`{"id":"job_debug","status":"failed"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "job.log"), []byte("submitted\nfailed error=\"fixture\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(t.TempDir(), "run1-debug.zip")
	out := runCLI(t, "debug", "bundle", "run1", "--store", store.Root, "--out", outPath, "--json")
	var report debugBundleReport
	decodeJSON(t, out, &report)
	if !report.OK || report.DatasetID != "run1" || report.Path != outPath || len(report.Files) == 0 {
		t.Fatalf("unexpected debug bundle report: %#v", report)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatal(err)
	}
	entries := readZipEntries(t, outPath)
	for _, want := range []string{
		"summary.json",
		"context.json",
		"doctor.json",
		"backend.json",
		"validation.json",
		"manifest.json",
		"frame_index_summary.json",
		"serve_smoke.json",
		"store/datasets/run1.yaml",
		"store/trajectory_index.json",
		"store/session_index.json",
		"store/indexes/run1-frame-index.json",
		"jobs/job_debug/status.json",
		"jobs/job_debug/job.log",
	} {
		if _, ok := entries[want]; !ok {
			t.Fatalf("debug bundle missing %s; entries=%v", want, sortedKeys(entries))
		}
	}
	var summary debugBundleReport
	decodeJSON(t, string(entries["summary.json"]), &summary)
	if !summary.OK || summary.DatasetID != "run1" {
		t.Fatalf("unexpected bundle summary: %#v", summary)
	}
	var validation datasetValidationReport
	decodeJSON(t, string(entries["validation.json"]), &validation)
	if !validation.OK || validation.ID != "run1" {
		t.Fatalf("unexpected validation bundle entry: %#v", validation)
	}
	var indexSummary debugFrameIndexSummary
	decodeJSON(t, string(entries["frame_index_summary.json"]), &indexSummary)
	if !indexSummary.OK || indexSummary.FrameCount != 4 || indexSummary.ChunkCount != 2 || !indexSummary.Materialized {
		t.Fatalf("unexpected frame index summary: %#v", indexSummary)
	}
}

func TestDemoCreateWritesRunnableJob(t *testing.T) {
	if !gromacs.New(gromacs.Options{}).Available() {
		t.Skip("GROMACS is not available")
	}
	dir := filepath.Join(t.TempDir(), "demo")
	out := runCLI(t, "demo", "create", "--out", dir, "--id", "demo-create", "--frames", "3", "--json")
	var report demoReport
	decodeJSON(t, out, &report)
	if report.Job == "" || report.Topology == "" || report.Trajectory == "" {
		t.Fatalf("demo create report missing files: %#v", report)
	}
	if err := validateMDSrvJobSchemaFile(report.Job); err != nil {
		t.Fatal(err)
	}
	explainOut := runCLI(t, "explain", report.Job, "--json")
	var explanation explainReport
	decodeJSON(t, explainOut, &explanation)
	if len(explanation.Warnings) != 0 || len(explanation.Plan) == 0 {
		t.Fatalf("demo create job is not cleanly explainable: %#v", explanation)
	}
}

func TestBatchJSONLResolvesRelativePaths(t *testing.T) {
	if !gromacs.New(gromacs.Options{}).Available() {
		t.Skip("GROMACS is not available")
	}
	dir := t.TempDir()
	runCLI(t, "demo", "create", "--out", filepath.Join(dir, "raw"), "--id", "batch-demo", "--frames", "3", "--json")
	batchPath := filepath.Join(dir, "jobs.jsonl")
	if err := os.WriteFile(batchPath, []byte(`{"id":"batch-run","topology":"raw/structure.gro","trajectory":"raw/trajectory.xtc"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runCLI(t, "batch", batchPath, "--store", filepath.Join(dir, "store"), "--force", "--json")
	var reports []batchReport
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var report batchReport
		if err := json.Unmarshal([]byte(line), &report); err != nil {
			t.Fatalf("decode batch report: %v\n%s", err, out)
		}
		reports = append(reports, report)
	}
	if len(reports) != 1 || reports[0].ID != "batch-run" || reports[0].Error != "" {
		t.Fatalf("unexpected batch reports: %#v", reports)
	}
	if _, err := os.Stat(filepath.Join(dir, "store", "datasets", "batch-run.yaml")); err != nil {
		t.Fatalf("batch manifest was not written: %v", err)
	}
}

func TestInstallLocalBuildsBinaryAndCompletions(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	completionDir := filepath.Join(dir, "completions")
	out := runCLI(t,
		"install", "local",
		"--home", repoPath(),
		"--bin-dir", binDir,
		"--completion-dir", completionDir,
		"--force",
		"--json",
	)
	var report installLocalReport
	decodeJSON(t, out, &report)
	if !report.OK || report.Binary == "" || len(report.Completions) != 3 {
		t.Fatalf("unexpected install report: %#v", report)
	}
	for _, path := range []string{
		report.Binary,
		filepath.Join(completionDir, "hlmdsrv.bash"),
		filepath.Join(completionDir, "_hlmdsrv"),
		filepath.Join(completionDir, "hlmdsrv.fish"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() == 0 {
			t.Fatalf("installed file is empty: %s", path)
		}
	}
	cmd := exec.Command(report.Binary, "schema", "job")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("installed binary failed: %v\n%s", err, output)
	}
}

func TestInstallLocalReplacesSymlinkItself(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	completionDir := filepath.Join(dir, "completions")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	externalDir := filepath.Join(dir, "external")
	if err := os.MkdirAll(externalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	externalTarget := filepath.Join(externalDir, "hlmdsrv")
	if err := os.WriteFile(externalTarget, []byte("stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(binDir, "hlmdsrv")
	if err := os.Symlink(externalTarget, linkPath); err != nil {
		t.Fatal(err)
	}
	out := runCLI(t,
		"install", "local",
		"--home", repoPath(),
		"--bin-dir", binDir,
		"--completion-dir", completionDir,
		"--force",
		"--json",
	)
	var report installLocalReport
	decodeJSON(t, out, &report)
	if !report.OK || !report.WasSymlink {
		t.Fatalf("expected symlink overwrite report: %#v", report)
	}
	if info, err := os.Lstat(linkPath); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("install left symlink in place: %s", linkPath)
	}
	raw, err := os.ReadFile(externalTarget)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "stale" {
		t.Fatalf("install followed symlink and modified external target")
	}
}

func TestSelfTestEndToEnd(t *testing.T) {
	if !gromacs.New(gromacs.Options{}).Available() {
		t.Skip("GROMACS is not available")
	}
	dir := filepath.Join(t.TempDir(), "self-test")
	out := runCLI(t, "self-test", "--out-dir", dir, "--json")
	var report selfTestReport
	decodeJSON(t, out, &report)
	if !report.OK || report.Root != dir || report.Job == "" || report.Store == "" || report.Static == "" || report.RunReport == "" {
		t.Fatalf("unexpected self-test report: %#v", report)
	}
	expected := []string{"doctor", "demo_create", "explain", "run", "validate_strict", "publish_static", "serve_smoke"}
	if len(report.Steps) != len(expected) {
		t.Fatalf("self-test steps = %#v", report.Steps)
	}
	for i, step := range report.Steps {
		if step.Name != expected[i] || !step.OK {
			t.Fatalf("self-test step %d = %#v", i, step)
		}
	}
	for _, path := range []string{report.Job, report.RunReport, filepath.Join(report.Store, "datasets", "self-test.yaml")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("self-test artifact %s: %v", path, err)
		}
	}
}

func TestRunJobSchemaRejectsUnknownFields(t *testing.T) {
	data := []byte(`version: mdsrv.job/v1
metadata:
  id: run1
inputs:
  topology:
    path: structure.gro
    format: gro
  trajectories:
    - path: traj.xtc
      format: xtc
unexpected: true
`)
	if err := validateMDSrvJobSchemaBytes(data, "job.yaml"); err == nil {
		t.Fatal("expected schema validation to reject unknown field")
	}
}

func TestRunPlanAndDryRunDoNotMutateStore(t *testing.T) {
	dir := t.TempDir()
	jobPath := filepath.Join(dir, "job.yaml")
	job := `
version: mdsrv.job/v1
metadata:
  id: planned
inputs:
  topology:
    path: missing.gro
    format: gro
  trajectories:
    - path: missing.xtc
      format: xtc
outputs:
  - type: mdsrvx
    path: planned.mdsrvx
`
	if err := os.WriteFile(jobPath, []byte(job), 0o644); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(dir, "store")
	out := runCLI(t, "run", jobPath, "--store", store, "--dry-run", "--json")
	var plan runPlanReport
	decodeJSON(t, out, &plan)
	if !plan.DryRun || len(plan.Steps) == 0 || plan.Steps[0].Action != "ingest" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Fatalf("dry-run created store: %v", err)
	}
}

func TestConfigProfileProvidesDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	t.Setenv("MDSRV_CONFIG", configPath)
	store := filepath.Join(dir, "profile-store")
	cache := filepath.Join(dir, "cache")
	runCLI(t, "config", "init", "--profile", "local", "--store", store, "--backend", "gromacs", "--gmx-command", "missing-profile-gmx", "--auth-token", "secret", "--cache", cache, "--timeout", "2m", "--job-prune-on-start", "--job-ttl", "1ns", "--force", "--json")
	jobPath := filepath.Join(dir, "job.yaml")
	job := `
version: mdsrv.job/v1
metadata:
  id: profiled
inputs:
  topology:
    path: missing.gro
    format: gro
  trajectories:
    - path: missing.xtc
      format: xtc
`
	if err := os.WriteFile(jobPath, []byte(job), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runCLI(t, "--profile", "local", "run", jobPath, "--plan", "--json")
	var plan runPlanReport
	decodeJSON(t, out, &plan)
	if plan.Store != store {
		t.Fatalf("profile store = %q, want %q", plan.Store, store)
	}
	gromacsDoctorOut := runCLI(t, "--profile", "local", "gromacs", "doctor", "--json")
	var gromacsDoctor struct {
		Command []string `json:"command"`
	}
	decodeJSON(t, gromacsDoctorOut, &gromacsDoctor)
	if len(gromacsDoctor.Command) == 0 || gromacsDoctor.Command[0] != "missing-profile-gmx" {
		t.Fatalf("profile gmx command was not applied to raw gromacs command: %#v", gromacsDoctor.Command)
	}
	oldJobDir := filepath.Join(store, mdsrv.JobsDir, "job_old")
	if err := os.MkdirAll(oldJobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldFinishedAt := time.Now().UTC().Add(-time.Hour)
	oldStatus := serverJobStatus{
		ID:         "job_old",
		Type:       "chunks",
		DatasetID:  "profiled",
		Status:     "succeeded",
		CreatedAt:  oldFinishedAt,
		FinishedAt: &oldFinishedAt,
		Request:    serverJobRequest{Type: "chunks", DatasetID: "profiled"},
	}
	rawStatus, err := json.Marshal(oldStatus)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldJobDir, "status.json"), rawStatus, 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, "--profile", "local", "serve", "smoke", "--workers", "1")
	if _, err := os.Stat(oldJobDir); !os.IsNotExist(err) {
		t.Fatalf("profile job retention settings did not prune old job: %v", err)
	}
}

func TestConfigInitRejectsMalformedConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("profiles:\n  broken: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCLIError("config", "init", "--config", configPath, "--force")
	if err == nil {
		t.Fatal("expected malformed config to fail")
	}
	if !strings.Contains(err.Error(), "did not find expected node content") {
		t.Fatalf("unexpected error for malformed config: %v", err)
	}
}

func TestAutomationJSONContracts(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	initOut := runCLI(t, "init", "--store", storeDir, "--json")
	var initReport map[string]any
	decodeJSON(t, initOut, &initReport)
	assertJSONHasKeys(t, initReport, "store", "directories", "trajectory_index", "session_index")

	jobPath := filepath.Join(dir, "job.yaml")
	initJobOut := runCLI(t, "init", "job", "--id", "contract", "--topology", "structure.gro", "--trajectory", "traj.xtc", "--chunks", "--out", jobPath, "--json")
	var initJobReport map[string]any
	decodeJSON(t, initJobOut, &initJobReport)
	assertJSONHasKeys(t, initJobReport, "path", "manifest")
	manifest, ok := initJobReport["manifest"].(map[string]any)
	if !ok {
		t.Fatalf("init job manifest is not an object: %#v", initJobReport)
	}
	assertJSONHasKeys(t, manifest, "version", "metadata", "inputs", "streaming")

	configPath := filepath.Join(dir, "config.yaml")
	configOut := runCLI(t, "config", "init", "--config", configPath, "--profile", "contract", "--store", storeDir, "--backend", "gromacs", "--force", "--json")
	var configReport map[string]any
	decodeJSON(t, configOut, &configReport)
	assertJSONHasKeys(t, configReport, "path", "profile", "config")
	configPathOut := runCLI(t, "config", "path", "--config", configPath, "--json")
	var configPathReport map[string]any
	decodeJSON(t, configPathOut, &configPathReport)
	assertJSONHasKeys(t, configPathReport, "path")

	planOut := runCLI(t, "run", jobPath, "--store", storeDir, "--plan", "--json")
	var plan map[string]any
	decodeJSON(t, planOut, &plan)
	assertJSONHasKeys(t, plan, "id", "store", "steps")
	steps, ok := plan["steps"].([]any)
	if !ok || len(steps) == 0 {
		t.Fatalf("run plan steps missing: %#v", plan)
	}
	firstStep, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("run plan step is not an object: %#v", steps[0])
	}
	assertJSONHasKeys(t, firstStep, "action", "target", "input", "output", "note")

	fixtureStore, _, _ := makeHTTPFixtureStore(t)
	validateOut := runCLI(t, "validate", "run1", "--store", fixtureStore.Root, "--json")
	var validation map[string]any
	decodeJSON(t, validateOut, &validation)
	assertJSONHasKeys(t, validation, "id", "files")
	validateStoreOut := runCLI(t, "validate", fixtureStore.Root, "--json")
	var storeValidation mdsrv.StoreDoctorReport
	decodeJSON(t, validateStoreOut, &storeValidation)
	if !storeValidation.OK || storeValidation.Store != fixtureStore.Root {
		t.Fatalf("validate store path returned unexpected report: %#v", storeValidation)
	}

	publishOut := runCLI(t, "publish", "static", "--store", fixtureStore.Root, "--out", filepath.Join(dir, "public"), "--verify", "--json")
	var publish map[string]any
	decodeJSON(t, publishOut, &publish)
	assertJSONHasKeys(t, publish, "store", "out", "files", "verification")

	gromacsOut := runCLI(t, "gromacs", "doctor", "--json")
	var gromacsReport map[string]any
	decodeJSON(t, gromacsOut, &gromacsReport)
	assertJSONHasKeys(t, gromacsReport, "command", "available")

	doctorOut := runCLI(t, "doctor", "--json")
	var doctor doctorReport
	decodeJSON(t, doctorOut, &doctor)
	if len(doctor.Checks) == 0 {
		t.Fatal("doctor JSON returned no checks")
	}
	var rawDoctor map[string]any
	decodeJSON(t, doctorOut, &rawDoctor)
	assertJSONHasKeys(t, rawDoctor, "ok", "checks")
}

func TestPublishStaticCopiesStore(t *testing.T) {
	store, _, _ := makeHTTPFixtureStore(t)
	outDir := filepath.Join(t.TempDir(), "public")
	out := runCLI(t, "publish", "static", "--store", store.Root, "--out", outDir, "--verify", "--json")
	var report mdsrv.StaticPublishReport
	decodeJSON(t, out, &report)
	if len(report.Files) == 0 {
		t.Fatalf("publish report has no files: %#v", report)
	}
	if report.Verification == nil || !report.Verification.OK || len(report.Verification.Checks) == 0 {
		t.Fatalf("publish verification did not run cleanly: %#v", report.Verification)
	}
	assertNoDuplicateStaticChecks(t, report.Verification.Checks)
	for _, path := range []string{"trajectory_index.json", filepath.Join("datasets", "run1.yaml")} {
		if _, err := os.Stat(filepath.Join(outDir, path)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(outDir, "topology", "run1.gro")); err != nil {
		t.Fatal(err)
	}
	verification, err := mdsrv.VerifyStaticPublish(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK || len(verification.Missing) == 0 {
		t.Fatalf("expected verification to find missing topology: %#v", verification)
	}
}

func TestPublishStaticRejectsOutputInsideStore(t *testing.T) {
	store, _, _ := makeHTTPFixtureStore(t)
	_, _, err := runCLIError("publish", "static", "--store", store.Root, "--out", filepath.Join(store.Root, "public"))
	if err == nil {
		t.Fatal("expected output inside store to fail")
	}
	if !strings.Contains(err.Error(), "outside the source store") {
		t.Fatalf("unexpected error for nested static publish: %v", err)
	}
}

func TestInitJobStarterWritesValidManifest(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "job.yaml")
	runCLI(t, "init", "job", "--id", "starter", "--name", "Starter", "--topology", "structure.gro", "--trajectory", "traj.xtc", "--chunks", "--out", outPath)
	if err := validateMDSrvJobSchemaFile(outPath); err != nil {
		t.Fatal(err)
	}
	manifest, err := mdsrv.LoadManifestFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Metadata.ID != "starter" || manifest.Inputs.Topology.Format != "gro" || manifest.Inputs.Trajectories[0].Format != "xtc" || !manifest.Streaming.MaterializeChunks {
		t.Fatalf("unexpected starter manifest: %#v", manifest)
	}
}

func TestStableExitCodeClassification(t *testing.T) {
	dir := t.TempDir()
	badJobPath := filepath.Join(dir, "bad-job.yaml")
	if err := os.WriteFile(badJobPath, []byte("version: mdsrv.job/v1\nunexpected: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCLIError("run", badJobPath, "--store", filepath.Join(dir, "store"))
	if err == nil {
		t.Fatal("expected invalid manifest to fail")
	}
	if ErrorCode(err) != string(codeInvalidManifest) || ExitCode(err) != 2 {
		t.Fatalf("invalid manifest classified as code=%s exit=%d err=%v", ErrorCode(err), ExitCode(err), err)
	}

	_, _, err = runCLIError("gromacs", "probe", filepath.Join(dir, "missing.xtc"), "--gmx-command", "missing-profile-gmx")
	if err == nil {
		t.Fatal("expected missing backend to fail")
	}
	if ErrorCode(err) != string(codeMissingBackend) || ExitCode(err) != 4 {
		t.Fatalf("missing backend classified as code=%s exit=%d err=%v", ErrorCode(err), ExitCode(err), err)
	}
}

func TestOpenAPISchemaMatchesExpectedServerSurface(t *testing.T) {
	schema := openAPISchema()
	rawPaths, ok := schema["paths"].(map[string]any)
	if !ok {
		t.Fatalf("openapi paths missing: %#v", schema["paths"])
	}
	expected := map[string][]string{
		"/health":                                          {"get"},
		"/version":                                         {"get"},
		"/capabilities":                                    {"get"},
		"/metrics":                                         {"get"},
		"/schema/manifest":                                 {"get"},
		"/schema/batch":                                    {"get"},
		"/schema/openapi":                                  {"get"},
		"/datasets":                                        {"get", "post"},
		"/datasets/{dataset_id}":                           {"get", "patch", "delete"},
		"/datasets/{dataset_id}/rename":                    {"post"},
		"/datasets/{dataset_id}/metadata":                  {"get"},
		"/datasets/{dataset_id}/topology":                  {"get"},
		"/datasets/{dataset_id}/trajectory":                {"get"},
		"/datasets/{dataset_id}/frames/count":              {"get"},
		"/datasets/{dataset_id}/frames/{frame}":            {"get"},
		"/datasets/{dataset_id}/frames/range":              {"get"},
		"/datasets/{dataset_id}/frames/index":              {"get", "post"},
		"/datasets/{dataset_id}/frames/chunks":             {"get", "post"},
		"/datasets/{dataset_id}/frames/chunks/{chunk}":     {"get"},
		"/datasets/{dataset_id}/selections":                {"get", "post"},
		"/datasets/{dataset_id}/selections/{selection_id}": {"get", "delete"},
		"/datasets/{dataset_id}/analyses":                  {"get", "post"},
		"/jobs":                                            {"get", "post"},
		"/jobs/{job_id}":                                   {"get"},
		"/jobs/stats":                                      {"get"},
		"/jobs/metrics":                                    {"get"},
		"/jobs/{job_id}/logs":                              {"get"},
		"/jobs/{job_id}/events":                            {"get"},
		"/jobs/{job_id}/cancel":                            {"post"},
		"/jobs/{job_id}/retry":                             {"post"},
		"/sessions":                                        {"get", "post"},
		"/trajectory_index.json":                           {"get"},
		"/session_index.json":                              {"get"},
	}
	for path, methods := range expected {
		rawPath, ok := rawPaths[path].(map[string]any)
		if !ok {
			t.Fatalf("openapi missing path %s", path)
		}
		for _, method := range methods {
			if _, ok := rawPath[method]; !ok {
				t.Fatalf("openapi path %s missing method %s", path, method)
			}
		}
	}
}

func TestExamplesValidateAgainstSchemas(t *testing.T) {
	for _, name := range []string{"mdsrv.job.yaml", "mdsrv-demo.job.yaml"} {
		exampleManifestPath := repoPath("examples", name)
		if err := validateMDSrvJobSchemaFile(exampleManifestPath); err != nil {
			t.Fatal(err)
		}
		manifest, err := mdsrv.LoadManifestFile(exampleManifestPath)
		if err != nil {
			t.Fatal(err)
		}
		if manifest.Metadata.ID == "" {
			t.Fatalf("example manifest %s missing id: %#v", name, manifest.Metadata)
		}
	}
	manifest, err := mdsrv.LoadManifestFile(repoPath("examples", "mdsrv.job.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Runtime.MaxAtoms == 0 || manifest.Runtime.MaxChunkBytes == 0 || manifest.Runtime.TimeoutSeconds == 0 {
		t.Fatalf("example manifest runtime limits did not decode: %#v", manifest.Runtime)
	}
	jobs, err := mdsrv.LoadBatchFile(repoPath("examples", "mdsrv.batch.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID == "" {
		t.Fatalf("example batch did not load: %#v", jobs)
	}
	for name, schema := range map[string]map[string]any{
		"manifest": manifestSchema(),
		"batch":    batchSchema(),
		"openapi":  openAPISchema(),
	} {
		data, err := json.Marshal(schema)
		if err != nil {
			t.Fatalf("%s schema did not marshal: %v", name, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("%s schema did not round-trip as JSON: %v", name, err)
		}
	}
}

func TestMDSrvJobSchemaGolden(t *testing.T) {
	data, err := json.MarshalIndent(manifestSchema(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	golden, err := os.ReadFile(repoPath("schema", "hlmdsrv-job-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(golden) {
		t.Fatal("generated MDsrv job schema does not match schema/hlmdsrv-job-v1.schema.json; run `npm run schema:mdsrv`")
	}
}

func TestRootHelpContract(t *testing.T) {
	var stdout, stderr bytes.Buffer
	a := app{stdout: &stdout, stderr: &stderr}
	root := a.rootCommand()
	root.SetArgs([]string{"--help"})
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("help failed: %v\n%s", err, stderr.String())
	}
	help := stdout.String()
	for _, command := range []string{"run", "dataset", "frames", "schema", "serve"} {
		if !strings.Contains(help, "\n  "+command+" ") && !strings.Contains(help, "\n  "+command+"\t") {
			t.Fatalf("root help missing visible command %q:\n%s", command, help)
		}
	}
	for _, hidden := range []string{"frame", "inspect", "gc"} {
		if strings.Contains(help, "\n  "+hidden+" ") || strings.Contains(help, "\n  "+hidden+"\t") {
			t.Fatalf("root help exposes deprecated command %q:\n%s", hidden, help)
		}
	}
}

func TestInstallBackendsGuideAndDoctorAreActionable(t *testing.T) {
	out := runCLI(t, "install", "backends", "--json")
	var guide map[string]any
	decodeJSON(t, out, &guide)
	if guide["gromacs"] == nil || guide["python"] == nil {
		t.Fatalf("install guide missing backend sections: %#v", guide)
	}
	doctorOut := runCLI(t, "doctor", "--json")
	var doctor doctorReport
	decodeJSON(t, doctorOut, &doctor)
	checks := doctor.Checks
	var sawPythonCommand, sawPythonBackend, sawRequired, sawRecommended bool
	for _, check := range checks {
		if check.Level == "" {
			t.Fatalf("doctor check missing level: %#v", check)
		}
		if !check.OK && check.Remediation == "" {
			t.Fatalf("failed doctor check missing remediation: %#v", check)
		}
		if check.Level == "required" {
			sawRequired = true
		}
		if check.Level == "recommended" {
			sawRecommended = true
		}
		if check.Name == "python command" {
			sawPythonCommand = true
		}
		if check.Name == "python trajectory backend" && (check.OK || check.Hint != "") {
			sawPythonBackend = true
		}
	}
	if !sawPythonCommand || !sawPythonBackend {
		t.Fatalf("doctor output is missing actionable backend checks: %#v", checks)
	}
	if !sawRequired || !sawRecommended {
		t.Fatalf("doctor output is missing graded checks: %#v", checks)
	}
}

func TestCLIHelpGoldens(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "root",
			args: []string{"--help"},
			want: []string{
				"Headless MDsrv dataset and session manager",
				"--profile string",
				"--timeout duration",
				"completion",
				"explain",
				"publish",
				"config",
			},
		},
		{
			name: "doctor",
			args: []string{"doctor", "--help"},
			want: []string{
				"Check local MDsrv headless prerequisites",
				"--strict",
				"--cache string",
				"--static-out string",
			},
		},
		{
			name: "explain",
			args: []string{"explain", "--help"},
			want: []string{
				"Explain a concept or resolve a job manifest plan",
				"--store string",
				"--strict",
			},
		},
		{
			name: "completion",
			args: []string{"completion", "--help"},
			want: []string{
				"Print shell completion scripts",
				"--out string",
				"--force",
			},
		},
		{
			name: "self-test",
			args: []string{"self-test", "--help"},
			want: []string{
				"Run local MDsrv headless smoke checks",
				"--out-dir string",
				"--frames int",
			},
		},
		{
			name: "version",
			args: []string{"version", "--help"},
			want: []string{
				"Report MDsrv headless CLI build provenance",
				"--json",
			},
		},
		{
			name: "capabilities",
			args: []string{"capabilities", "--help"},
			want: []string{
				"Report MDsrv headless backend and feature capabilities",
				"--store string",
				"--gmx-command string",
			},
		},
		{
			name: "docs",
			args: []string{"docs", "--help"},
			want: []string{
				"Generate MDsrv headless CLI reference documentation",
				"--out string",
			},
		},
		{
			name: "run",
			args: []string{"run", "--help"},
			want: []string{
				"Run an end-to-end MDsrv headless job manifest",
				"--plan",
				"--dry-run",
				"--cache string",
				"--report string",
				"--probe-timeout duration",
				"--analysis-timeout duration",
			},
		},
		{
			name: "init-job",
			args: []string{"init", "job", "--help"},
			want: []string{
				"Create a starter mdsrv.job/v1 manifest",
				"--topology string",
				"--trajectory string",
				"--chunks",
			},
		},
		{
			name: "demo-create",
			args: []string{"demo", "create", "--help"},
			want: []string{
				"Create a tiny trajectory and runnable job manifest",
				"--job string",
				"--frames int",
			},
		},
		{
			name: "config-init",
			args: []string{"config", "init", "--help"},
			want: []string{
				"Create or update a CLI profile",
				"--profile string",
				"--store string",
				"--timeout duration",
			},
		},
		{
			name: "install-local",
			args: []string{"install", "local", "--help"},
			want: []string{
				"Build and install hlmdsrv plus shell completions",
				"--bin-dir string",
				"--completion-dir string",
				"--force",
			},
		},
		{
			name: "publish-static",
			args: []string{"publish", "static", "--help"},
			want: []string{
				"Copy a store into a read-only static directory",
				"--out string",
				"--force",
				"--store string",
				"--verify",
			},
		},
		{
			name: "debug-bundle",
			args: []string{"debug", "bundle", "--help"},
			want: []string{
				"Write a small zip archive with store, backend, validation, and server diagnostics",
				"--out string",
				"--skip-smoke",
				"--max-logs int",
			},
		},
		{
			name: "serve",
			args: []string{"serve", "--help"},
			want: []string{
				"Serve a headless MDsrv store over HTTP",
				"--auth-token string",
				"--job-timeout duration",
				"--read-only",
				"--request-timeout duration",
				"--workers int",
			},
		},
		{
			name: "serve-smoke",
			args: []string{"serve", "smoke", "--help"},
			want: []string{
				"Start the HTTP handler in-process and verify key routes",
				"--store string",
				"--read-only",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := runCLI(t, tt.args...)
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Fatalf("help output missing %q\n%s", want, out)
				}
			}
		})
	}
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	a := app{stdout: &stdout, stderr: &stderr}
	root := a.rootCommand()
	root.SetArgs(args)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("hlmdsrv %s failed: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func runCLIError(args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	a := app{stdout: &stdout, stderr: &stderr}
	root := a.rootCommand()
	root.SetArgs(args)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	err := root.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

func decodeJSON(t *testing.T, data string, out any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), out); err != nil {
		t.Fatalf("decode json: %v\n%s", err, data)
	}
}

func readZipEntries(t *testing.T, path string) map[string][]byte {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entries := map[string][]byte{}
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name] = data
	}
	return entries
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assertPythonBackendGolden(t *testing.T, store string, backend string, expectedBackend string, expectedUnit string, scale float64) {
	t.Helper()
	frameOut := runCLI(t, "frames", "get", "run1", "0", "--store", store, "--backend", backend, "--atom-subset", "first-two", "--format", "json")
	var frame mdsrv.Frame
	decodeJSON(t, frameOut, &frame)
	if frame.Backend != expectedBackend || len(frame.Coordinates) != 2 {
		t.Fatalf("%s frame = %#v", backend, frame)
	}
	tracePath := filepath.Join(t.TempDir(), backend+"-distance.csv")
	runCLI(t, "analyze", "distance", "run1", "--store", store, "--backend", backend, "--a", "1", "--b", "2", "--out", tracePath)
	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	assertDemoDistanceGolden(t, string(data), 6, expectedUnit, scale)
}

func assertDemoDistanceGolden(t *testing.T, csvData string, frameCount int, expectedUnit string, scale float64) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(csvData), "\n")
	if len(lines) != frameCount+1 {
		t.Fatalf("distance trace line count = %d, want %d:\n%s", len(lines), frameCount+1, csvData)
	}
	for frame := 0; frame < frameCount; frame++ {
		fields := strings.Split(lines[frame+1], ",")
		if len(fields) < 4 {
			t.Fatalf("bad trace row %q", lines[frame+1])
		}
		if fields[3] != expectedUnit {
			t.Fatalf("trace unit = %q, want %q", fields[3], expectedUnit)
		}
		gotFrame, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatal(err)
		}
		if gotFrame != frame {
			t.Fatalf("trace frame = %d, want %d", gotFrame, frame)
		}
		got, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			t.Fatal(err)
		}
		shift := 0.01 * float64(frame)
		want := scale * math.Sqrt(0.1*0.1+shift*shift)
		if math.Abs(got-want) > 1e-6 {
			t.Fatalf("frame %d distance = %.12g, want %.12g\n%s", frame, got, want, csvData)
		}
	}
}

// TestFrameEndpointsRejectOutOfRangeIndices locks in that requesting a frame or
// frame range outside the dataset's frame count returns a clean 400 instead of
// letting the backend fail deep in the handler and surfacing a confusing 503
// that leaks the backend's internal error. The fixture has 4 frames (0..3).
func TestFrameEndpointsRejectOutOfRangeIndices(t *testing.T) {
	store, _, _ := makeHTTPFixtureStore(t)
	mux := http.NewServeMux()
	registerHandlersWithOptions(mux, store, serverOptions{Backend: "gromacs", GromacsCommand: "missing-gmx", MaxFrameRange: 256})
	server := httptest.NewServer(mux)
	defer server.Close()

	// Out-of-range single frame, negative frame, and range past the end: all 400.
	assertStatus(t, server, http.MethodGet, "/datasets/run1/frames/99?format=json", "", http.StatusBadRequest)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/frames/-1?format=json", "", http.StatusBadRequest)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/frames/range?start=0&stop=4", "", http.StatusBadRequest)
	assertStatus(t, server, http.MethodGet, "/datasets/run1/frames/range?start=10&stop=12", "", http.StatusBadRequest)
}

func makeHTTPFixtureStore(t *testing.T) (mdsrv.Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	topology := filepath.Join(dir, "structure.gro")
	trajectory := filepath.Join(dir, "traj.xtc")
	if err := os.WriteFile(topology, []byte("topology"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trajectory, []byte("trajectory"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := mdsrv.OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Ingest(mdsrv.IngestOptions{ID: "run1", Name: "Run 1", Topology: topology, Trajectory: trajectory})
	if err != nil {
		t.Fatal(err)
	}
	m.Inputs.Trajectories[0].AtomCount = 3
	m.Inputs.Trajectories[0].FrameCount = 4
	m.Inputs.Trajectories[0].TimeStart = 0
	m.Inputs.Trajectories[0].TimeEnd = 3
	m.Inputs.Trajectories[0].TimeStep = 1
	m.Streaming.FrameIndex = filepath.ToSlash(filepath.Join(mdsrv.IndexesDir, "run1-frame-index.json"))
	if err := mdsrv.WriteManifestFile(store.ManifestPath("run1"), m); err != nil {
		t.Fatal(err)
	}
	index := mdsrv.FrameIndex{
		DatasetID:       "run1",
		FrameCount:      4,
		AtomCount:       3,
		TimeEnd:         3,
		TimeStep:        1,
		ChunkSizeFrames: 2,
		Frames: []mdsrv.FramePoint{
			{Index: 0, Time: 0},
			{Index: 1, Time: 1},
			{Index: 2, Time: 2},
			{Index: 3, Time: 3},
		},
		Chunks: []mdsrv.FrameChunk{
			{Index: 0, Start: 0, Stop: 2, Path: filepath.ToSlash(filepath.Join(mdsrv.ChunksDir, "run1", "chunk-000000.json")), Encoding: mdsrv.FrameChunkEncodingJSON},
			{Index: 1, Start: 2, Stop: 4},
		},
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(store.Root, mdsrv.IndexesDir, "run1-frame-index.json")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	chunk := mdsrv.FrameChunkData{
		DatasetID: "run1",
		Chunk:     0,
		Start:     0,
		Stop:      1,
		Encoding:  mdsrv.FrameChunkEncodingJSON,
		Frames: []mdsrv.Frame{{
			Backend:        "fixture",
			Frame:          0,
			Time:           0,
			TimeUnit:       "ps",
			CoordinateUnit: "nm",
			Coordinates:    [][3]float32{{0.1, 0.1, 0.1}},
		}},
	}
	chunkBytes, _, err := mdsrv.EncodeFrameChunk(chunk)
	if err != nil {
		t.Fatal(err)
	}
	chunkPath := filepath.Join(store.Root, mdsrv.ChunksDir, "run1", "chunk-000000.json")
	if err := os.MkdirAll(filepath.Dir(chunkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chunkPath, chunkBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return store, topology, trajectory
}

func assertStatus(t *testing.T, server *httptest.Server, method, path, body string, status int) {
	t.Helper()
	resp := doRequest(t, server, method, path, body)
	defer resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("%s %s: status=%d want=%d", method, path, resp.StatusCode, status)
	}
}

func assertRouteExists(t *testing.T, server *httptest.Server, method, path string, body ...string) {
	t.Helper()
	requestBody := ""
	if len(body) > 0 {
		requestBody = body[0]
	}
	resp := doRequest(t, server, method, path, requestBody)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		t.Fatalf("%s %s: route status=%d", method, path, resp.StatusCode)
	}
}

func doRequest(t *testing.T, server *httptest.Server, method, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func escapeJSON(value string) string {
	data, _ := json.Marshal(value)
	return strings.Trim(string(data), `"`)
}

func assertJSONKeys(t *testing.T, value map[string]any, keys ...string) {
	t.Helper()
	if len(value) != len(keys) {
		t.Fatalf("json keys = %#v, want %v", value, keys)
	}
	for _, key := range keys {
		if _, ok := value[key]; !ok {
			t.Fatalf("json missing key %q: %#v", key, value)
		}
	}
}

func assertJSONHasKeys(t *testing.T, value map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := value[key]; !ok {
			t.Fatalf("json missing key %q: %#v", key, value)
		}
	}
}

func assertNoDuplicateStaticChecks(t *testing.T, checks []mdsrv.StaticPublishCheck) {
	t.Helper()
	seen := map[string]bool{}
	for _, check := range checks {
		key := check.Kind + "\x00" + check.Path
		if seen[key] {
			t.Fatalf("duplicate static publish check %s/%s in %#v", check.Kind, check.Path, checks)
		}
		seen[key] = true
	}
}

func assertNoDuplicateRunArtifacts(t *testing.T, artifacts []runArtifact) {
	t.Helper()
	seen := map[string]bool{}
	for _, artifact := range artifacts {
		key := artifact.Type + "\x00" + artifact.Path
		if seen[key] {
			t.Fatalf("duplicate run artifact %s/%s in %#v", artifact.Type, artifact.Path, artifacts)
		}
		seen[key] = true
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForHTTPStatus(t *testing.T, url string, status int) {
	t.Helper()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	var lastStatus int
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			lastStatus = resp.StatusCode
			_ = resp.Body.Close()
			if lastStatus == status {
				return
			}
		} else {
			lastErr = err
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s status %d; last_status=%d last_error=%v", url, status, lastStatus, lastErr)
}

func repoPath(parts ...string) string {
	all := append([]string{"..", ".."}, parts...)
	return filepath.Join(all...)
}

func irregularDemoGRO(times []float64) string {
	var b strings.Builder
	for frame, timeValue := range times {
		shift := 0.01 * float64(frame)
		fmt.Fprintf(&b, "Irregular demo t= %.3f\n", timeValue)
		fmt.Fprintf(&b, "%5d\n", 3)
		fmt.Fprintf(&b, "%5d%-5s%5s%5d%8.3f%8.3f%8.3f\n", 1, "MOL", "C1", 1, 0.100+shift, 0.100, 0.100)
		fmt.Fprintf(&b, "%5d%-5s%5s%5d%8.3f%8.3f%8.3f\n", 1, "MOL", "O1", 2, 0.200+shift, 0.100+shift, 0.100)
		fmt.Fprintf(&b, "%5d%-5s%5s%5d%8.3f%8.3f%8.3f\n", 1, "MOL", "H1", 3, 0.300+shift, 0.100, 0.100+shift)
		fmt.Fprintln(&b, "   1.00000   1.00000   1.00000")
	}
	return b.String()
}
