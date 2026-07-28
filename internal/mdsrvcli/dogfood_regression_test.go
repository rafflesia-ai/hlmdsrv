package mdsrvcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

// Regression tests for the defects found by dogfooding the carved-out CLI. Each
// asserts the fixed behavior and would fail against the pre-fix code.

type executeResult struct {
	stdout string
	stderr string
	err    error
}

func execCLI(args ...string) executeResult {
	var stdout, stderr bytes.Buffer
	a := app{stdout: &stdout, stderr: &stderr}
	err := a.execute(context.Background(), args)
	return executeResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func decodeEnvelope(t *testing.T, payload string) errorEnvelope {
	t.Helper()
	var envelope errorEnvelope
	decoder := json.NewDecoder(strings.NewReader(payload))
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("stdout is not a JSON error envelope: %v\npayload:\n%s", err, payload)
	}
	// stdout must carry exactly one document: appending an envelope to a report a
	// command already wrote produced concatenated JSON that no consumer can parse.
	if decoder.More() {
		t.Fatalf("stdout carries more than one JSON document:\n%s", payload)
	}
	return envelope
}

// Finding #1: --json wrote nothing at all to stdout on every failure path, so an
// agent told to branch on error.code got an empty stream.
func TestJSONErrorEnvelopeIsWrittenToStdout(t *testing.T) {
	store := filepath.Join(t.TempDir(), "missing-store")
	result := execCLI("frames", "count", "nope", "--store", store, "--json")
	if result.err == nil {
		t.Fatal("expected an error for a dataset that does not exist")
	}
	if strings.TrimSpace(result.stdout) == "" {
		t.Fatal("no JSON envelope on stdout; --json must emit a machine-readable failure")
	}
	envelope := decodeEnvelope(t, result.stdout)
	if envelope.OK {
		t.Errorf("envelope ok = true, want false")
	}
	if envelope.Error.Code != string(codeMissingInput) {
		t.Errorf("code = %q, want %q", envelope.Error.Code, codeMissingInput)
	}
	if envelope.Error.ExitCode != 3 {
		t.Errorf("exit_code = %d, want 3", envelope.Error.ExitCode)
	}
	if envelope.Command != "frames count" {
		t.Errorf("command = %q, want %q", envelope.Command, "frames count")
	}
	if envelope.Timestamp == "" {
		t.Error("timestamp is empty")
	}
}

// Without --json the failure stays on stderr and stdout stays clean, so shell
// pipelines are unaffected by the envelope.
func TestPlainErrorStillGoesToStderr(t *testing.T) {
	store := filepath.Join(t.TempDir(), "missing-store")
	result := execCLI("frames", "count", "nope", "--store", store)
	if result.err == nil {
		t.Fatal("expected an error")
	}
	if strings.TrimSpace(result.stdout) != "" {
		t.Errorf("stdout should be empty without --json, got:\n%s", result.stdout)
	}
	if !strings.Contains(result.stderr, string(codeMissingInput)) {
		t.Errorf("stderr should name the code, got:\n%s", result.stderr)
	}
}

// wantsJSON honors pflag's last-wins semantics so the error path cannot desync
// from the success path on a contradictory invocation.
func TestWantsJSONLastWins(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"version"}, false},
		{[]string{"version", "--json"}, true},
		{[]string{"version", "--json=false"}, false},
		{[]string{"version", "--json", "--json=false"}, false},
		{[]string{"version", "--json=false", "--json"}, true},
		{[]string{"version", "--json=0"}, false},
	} {
		if got := wantsJSON(tc.args); got != tc.want {
			t.Errorf("wantsJSON(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

// Finding #8: cobra usage failures fell through to internal_error (exit 1),
// whose documented meaning is "unclassified, report it" — so a typo'd flag told
// the caller to file a bug.
func TestUsageErrorsAreInvalidInput(t *testing.T) {
	for _, args := range [][]string{
		{"version", "--definitely-not-a-flag", "--json"},
		{"frobnicate", "--json"},
		{"--timeout", "notaduration", "version", "--json"},
	} {
		result := execCLI(args...)
		if result.err == nil {
			t.Fatalf("hlmdsrv %s: expected an error", strings.Join(args, " "))
		}
		if got := ExitCode(result.err); got != 2 {
			t.Errorf("hlmdsrv %s: exit = %d, want 2", strings.Join(args, " "), got)
		}
		envelope := decodeEnvelope(t, result.stdout)
		if envelope.Error.Code != string(codeInvalidInput) {
			t.Errorf("hlmdsrv %s: code = %q, want %q", strings.Join(args, " "), envelope.Error.Code, codeInvalidInput)
		}
	}
}

func TestIsUsageError(t *testing.T) {
	if !isUsageError(errors.New(`unknown flag: --nope`)) {
		t.Error("unknown flag should be a usage error")
	}
	if !isUsageError(errors.New(`accepts 1 arg(s), received 0`)) {
		t.Error("arg-count failure should be a usage error")
	}
	if isUsageError(errors.New("sha256 mismatch")) {
		t.Error("a runtime failure must not be treated as a usage error")
	}
}

// Finding #6: --timeout built a deadline context that the pure-Go commands never
// consulted, so an exhausted budget produced a full success report at exit 0.
func TestExhaustedTimeoutFailsBeforeTheCommandRuns(t *testing.T) {
	store := t.TempDir()
	if result := execCLI("init", "--store", store, "--json"); result.err != nil {
		t.Fatalf("init: %v", result.err)
	}
	result := execCLI("--timeout", "1ns", "store", "doctor", "--store", store, "--json")
	if result.err == nil {
		t.Fatal("an exhausted --timeout must not report success")
	}
	if got := ExitCode(result.err); got != 5 {
		t.Errorf("exit = %d, want 5", got)
	}
	envelope := decodeEnvelope(t, result.stdout)
	if envelope.Error.Code != string(codeBackendTimeout) {
		t.Errorf("code = %q, want %q", envelope.Error.Code, codeBackendTimeout)
	}
}

func TestGenerousTimeoutDoesNotInterfere(t *testing.T) {
	store := t.TempDir()
	if result := execCLI("init", "--store", store, "--json"); result.err != nil {
		t.Fatalf("init: %v", result.err)
	}
	if result := execCLI("--timeout", "5m", "store", "doctor", "--store", store, "--json"); result.err != nil {
		t.Fatalf("a generous timeout must not fail the run: %v\nstderr:\n%s", result.err, result.stderr)
	}
}

// Finding #2: `export --out <one of the run's own inputs> --force` truncated the
// source the instant the output was opened, and reported success.
func TestEnsureOutputPathRejectsSelfOverwrite(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "topology.gro")
	if err := os.WriteFile(input, []byte("original contents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ensureOutputPathAgainst(input, true, input)
	if err == nil {
		t.Fatal("writing an output over its own input must be refused")
	}
	if ErrorCode(err) != string(codeInvalidInput) {
		t.Errorf("code = %q, want %q", ErrorCode(err), codeInvalidInput)
	}
	data, readErr := os.ReadFile(input)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "original contents\n" {
		t.Errorf("input was modified: %q", string(data))
	}
}

// A hardlink aliases the same inode, so a literal path comparison would miss it.
func TestEnsureOutputPathRejectsSelfOverwriteViaHardlink(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "topology.gro")
	alias := filepath.Join(dir, "alias.gro")
	if err := os.WriteFile(input, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(input, alias); err != nil {
		t.Skipf("hardlinks unsupported here: %v", err)
	}
	if err := ensureOutputPathAgainst(alias, true, input); err == nil {
		t.Fatal("a hardlink aliasing an input must be refused")
	}
}

func TestEnsureOutputPathAllowsAnUnrelatedTarget(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "in.gro")
	if err := os.WriteFile(input, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureOutputPathAgainst(filepath.Join(dir, "out.xtc"), false, input); err != nil {
		t.Fatalf("an unrelated output path must be accepted: %v", err)
	}
}

// Finding #3: an existing FIFO/device output was unlinked and replaced with a
// regular file, destroying a pipe another process may have been reading.
func TestRejectNonRegularOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no FIFOs on windows")
	}
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}
	err := rejectNonRegularOutput(fifo)
	if err == nil {
		t.Fatal("a FIFO output must be refused")
	}
	if ErrorCode(err) != string(codeInvalidInput) {
		t.Errorf("code = %q, want %q", ErrorCode(err), codeInvalidInput)
	}
	info, statErr := os.Lstat(fifo)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Error("the FIFO was replaced")
	}
	// A regular file and a missing path are both fine.
	regular := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rejectNonRegularOutput(regular); err != nil {
		t.Errorf("a regular file must be accepted: %v", err)
	}
	if err := rejectNonRegularOutput(filepath.Join(dir, "absent")); err != nil {
		t.Errorf("a missing path must be accepted: %v", err)
	}
}

// Finding #8: a missing or directory input was handed to gmx, which failed with
// its own banner and got classified internal_error (exit 1).
func TestRequireInputFile(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "ok.gro")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requireInputFile(regular); err != nil {
		t.Errorf("a readable regular file must be accepted: %v", err)
	}
	for name, path := range map[string]string{
		"missing":   filepath.Join(dir, "absent.gro"),
		"directory": dir,
		"empty":     "   ",
	} {
		err := requireInputFile(path)
		if err == nil {
			t.Errorf("%s input must be rejected", name)
			continue
		}
		if ErrorCode(err) != string(codeInvalidInput) {
			t.Errorf("%s: code = %q, want %q", name, ErrorCode(err), codeInvalidInput)
		}
	}
}

// Completeness sweep after the first fix pass: the guards had only been wired at
// the sites probed by hand, so the dataset-oriented commands (frames get, pack,
// debug bundle) still truncated a store's own topology at exit 0.
func TestRejectDatasetInputOverwrite(t *testing.T) {
	dir := t.TempDir()
	topology := filepath.Join(dir, "structure.gro")
	trajectory := filepath.Join(dir, "traj.xtc")
	for _, path := range []string{topology, trajectory} {
		if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := mdsrv.OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Ingest(mdsrv.IngestOptions{ID: "d", Topology: topology, Trajectory: trajectory})
	if err != nil {
		t.Fatal(err)
	}

	storedTopology, err := store.SafeResolvePath(m.Inputs.Topology.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectDatasetInputOverwrite(store, m, storedTopology); err == nil {
		t.Fatal("an output naming the dataset's own topology must be refused")
	} else if ErrorCode(err) != string(codeInvalidInput) {
		t.Errorf("code = %q, want %q", ErrorCode(err), codeInvalidInput)
	}

	// An unrelated path, and an output that does not exist yet, are both fine.
	if err := rejectDatasetInputOverwrite(store, m, filepath.Join(dir, "elsewhere.xtc")); err != nil {
		t.Errorf("an unrelated output must be accepted: %v", err)
	}
	if err := rejectDatasetInputOverwrite(store, m, ""); err != nil {
		t.Errorf("an empty output must be accepted: %v", err)
	}
}

// An external tool invoked through a caller-supplied command can exit 0 without
// producing anything (`--gmx-command /usr/bin/true`), which left export and the
// gromacs bridge reporting success with an `output` path that did not exist.
func TestRequireProducedFile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "never-written.xtc")
	if err := requireProducedFile(missing, "gromacs export"); err == nil {
		t.Fatal("a missing output must not pass as success")
	} else if ErrorCode(err) != string(codeRenderFailed) {
		t.Errorf("code = %q, want %q", ErrorCode(err), codeRenderFailed)
	}

	empty := filepath.Join(dir, "empty.xtc")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requireProducedFile(empty, "gromacs export"); err == nil {
		t.Error("an empty output must not pass as success")
	}

	real := filepath.Join(dir, "real.xtc")
	if err := os.WriteFile(real, []byte("frames"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requireProducedFile(real, "gromacs export"); err != nil {
		t.Errorf("a non-empty output must be accepted: %v", err)
	}
}

// Reading a FIFO blocks in open(2) exactly as writing one does, so the config
// path has to be screened on the way in as well as on the way out.
func TestRejectNonRegularPathGuardsBothDirections(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no FIFOs on windows")
	}
	fifo := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}
	for _, verb := range []string{"read", "write to"} {
		if err := rejectNonRegularPath(fifo, verb); err == nil {
			t.Errorf("a FIFO must be refused for %q", verb)
		}
	}
	if _, err := loadConfig(fifo); err == nil {
		t.Error("loadConfig must refuse a FIFO rather than block on it")
	}
}

// Third-pass audit: a directory stats with a non-zero size, so the "did the tool
// produce the file?" check passed an --out that was an empty directory and
// reported a trajectory export that produced nothing.
func TestRequireProducedFileRejectsADirectory(t *testing.T) {
	dir := t.TempDir()
	if err := requireProducedFile(dir, "gromacs export"); err == nil {
		t.Fatal("a directory must not pass as a produced file")
	} else if ErrorCode(err) != string(codeInvalidInput) {
		t.Errorf("code = %q, want %q", ErrorCode(err), codeInvalidInput)
	}
}

// Every caller of ensureOutputPath writes a single file, so a directory target
// must be refused up front rather than handed to the tool.
func TestEnsureOutputPathRejectsADirectory(t *testing.T) {
	dir := t.TempDir()
	err := ensureOutputPathAgainst(dir, true)
	if err == nil {
		t.Fatal("a directory --out must be refused")
	}
	if ErrorCode(err) != string(codeInvalidInput) {
		t.Errorf("code = %q, want %q", ErrorCode(err), codeInvalidInput)
	}
}

// A --store pointing at a missing path or a regular file used to read as an
// empty-but-valid store: `list datasets` printed null and exited 0, which is
// indistinguishable from a real store holding no datasets.
func TestStoreRequireExisting(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "afile")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing, err := mdsrv.OpenStore(filepath.Join(dir, "absent"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missing.ListDatasets(); err == nil {
		t.Error("listing a store that does not exist must fail, not report zero datasets")
	} else if ErrorCode(err) != string(codeMissingInput) {
		t.Errorf("missing store: code = %q, want %q", ErrorCode(err), codeMissingInput)
	}

	notDir, err := mdsrv.OpenStore(regular)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := notDir.ListDatasets(); err == nil {
		t.Error("listing a store that is a regular file must fail")
	} else if ErrorCode(err) != string(codeInvalidInput) {
		t.Errorf("file store: code = %q, want %q", ErrorCode(err), codeInvalidInput)
	}

	real, err := mdsrv.OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	if err := real.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := real.ListDatasets(); err != nil {
		t.Errorf("a real store must list cleanly: %v", err)
	}
}

// The classifier decides the exit code from the message, so these shapes must not
// fall through to internal_error, documented as "unclassified, report it".
func TestClassifierCoversPathShapeFailures(t *testing.T) {
	for message, want := range map[string]errorCode{
		"store /x is not a directory":                codeInvalidInput,
		"/x is a directory, not a file":              codeInvalidInput,
		"/x is not a regular file":                   codeInvalidInput,
		"store /x does not exist":                    codeMissingInput,
		`gromacs command "true" is not usable: nope`: codeMissingBackend,
	} {
		if got := classifyErrorCode(errors.New(message)); got != want {
			t.Errorf("classify(%q) = %q, want %q", message, got, want)
		}
	}
}

// Fourth pass: every HTTP status the server returns must carry a typed code.
// 429 (job queue full) fell through to a generic "error" and 502 to
// "internal_error" — and backpressure is the one response a client most needs to
// branch on, because it is the retryable one.
func TestHTTPStatusCodesAreTyped(t *testing.T) {
	for status, want := range map[int]string{
		http.StatusBadRequest:          "bad_request",
		http.StatusUnauthorized:        "unauthorized",
		http.StatusForbidden:           "forbidden",
		http.StatusNotFound:            "not_found",
		http.StatusMethodNotAllowed:    "method_not_allowed",
		http.StatusTooManyRequests:     "too_many_requests",
		http.StatusBadGateway:          "bad_gateway",
		http.StatusServiceUnavailable:  "service_unavailable",
		http.StatusInternalServerError: "internal_error",
	} {
		if got := httpStatusCode(status); got != want {
			t.Errorf("httpStatusCode(%d) = %q, want %q", status, got, want)
		}
	}
	// Every status the server actually writes must be typed; none may fall through
	// to the generic "error" bucket.
	for _, status := range []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusInternalServerError,
	} {
		if httpStatusCode(status) == "error" {
			t.Errorf("status %d still falls through to the generic \"error\" code", status)
		}
	}
}

// Explicit flag-range validation is caller-fixable, so it must not land in
// internal_error, which tells the caller to file a bug about their own typo. A
// dead --server is likewise the backend being unavailable, not an internal fault.
func TestClassifierCoversFlagAndNetworkFailures(t *testing.T) {
	for message, want := range map[string]errorCode{
		"--frames must be at least 2":                  codeInvalidInput,
		"--workers cannot be negative":                 codeInvalidInput,
		"--iterations must be at least 1":              codeInvalidInput,
		"x.gro is not a valid .mdsrvx archive":         codeInvalidInput,
		"dial tcp 127.0.0.1:1: connection refused":     codeMissingBackend,
		"Get \"http://nope/\": dial tcp: no such host": codeMissingBackend,
		"--out is required":                            codeMissingInput,
	} {
		if got := classifyErrorCode(errors.New(message)); got != want {
			t.Errorf("classify(%q) = %q, want %q", message, got, want)
		}
	}
}

// Fifth pass — a hole in the error-envelope fix itself. Some commands write
// their --json report and *then* return an error (compat check, publish static
// --verify). Appending an envelope after that report put TWO JSON documents on
// stdout, which fails a consumer's parse outright — worse than emitting nothing.
func TestFailureAfterAReportDoesNotAppendASecondDocument(t *testing.T) {
	// A regular file as --store fails deterministically. A merely-absent path does
	// not: compat check would simply create the store there.
	store := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(store, []byte("not a store"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := execCLI("compat", "check", "--store", store, "--json")
	if result.err == nil {
		t.Fatal("compat check against a store that is a regular file should fail")
	}
	// decodeEnvelope rejects a second document; here the single document is the
	// command's own report, which carries its own ok:false.
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(result.stdout))
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, result.stdout)
	}
	if decoder.More() {
		t.Fatalf("stdout carries more than one JSON document:\n%s", result.stdout)
	}
	if ok, present := payload["ok"].(bool); !present || ok {
		t.Errorf("the report should carry ok:false, got %v", payload["ok"])
	}
	// The error still has to reach the operator, just not on stdout.
	if strings.TrimSpace(result.stderr) == "" {
		t.Error("the failure should be reported on stderr when stdout is already used")
	}
}

// A non-positive --chunk-size was silently coerced to "everything in one chunk",
// so a caller who asked for specific chunking got something else with no signal.
func TestRequirePositiveChunkSize(t *testing.T) {
	for _, size := range []int{0, -1, -128} {
		err := requirePositiveChunkSize(size)
		if err == nil {
			t.Errorf("--chunk-size %d must be rejected", size)
			continue
		}
		if ErrorCode(err) != string(codeInvalidInput) {
			t.Errorf("--chunk-size %d: code = %q, want %q", size, ErrorCode(err), codeInvalidInput)
		}
	}
	for _, size := range []int{1, 2, 128} {
		if err := requirePositiveChunkSize(size); err != nil {
			t.Errorf("--chunk-size %d must be accepted: %v", size, err)
		}
	}
}

// A tripped resource limit is the policy working as configured, not an
// unclassified fault; a check that ran and did not pass is likewise a validation
// outcome. Both were landing in internal_error.
func TestClassifierCoversLimitsAndChecks(t *testing.T) {
	for message, want := range map[string]errorCode{
		"trajectory has 6 frames, exceeding max_frames=1":              codeValidationFailed,
		"trajectory has 3 atoms, exceeding max_atoms=1":                codeValidationFailed,
		"chunk-000000.json is 1253 bytes, exceeding max_chunk_bytes=1": codeValidationFailed,
		"compatibility check failed":                                   codeValidationFailed,
		"lstat /tmp/afile: not a directory":                            codeInvalidInput,
	} {
		if got := classifyErrorCode(errors.New(message)); got != want {
			t.Errorf("classify(%q) = %q, want %q", message, got, want)
		}
	}
}

// Finding #9: publishing a store that does not exist reported success with
// files:null and left an empty output directory behind.
func TestPublishRejectsMissingStore(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "static")
	result := execCLI("publish", "static", "--store", filepath.Join(dir, "nope"), "--out", out, "--json")
	if result.err == nil {
		t.Fatal("publishing a nonexistent store must fail")
	}
	if got := ExitCode(result.err); got != 3 {
		t.Errorf("exit = %d, want 3", got)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("an output directory was created for a store that does not exist")
	}
}
