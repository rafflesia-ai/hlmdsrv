package mdsrv

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafflesia-ai/hlmdsrv/internal/gromacs"
)

func TestIngestWritesManifestAndIndexes(t *testing.T) {
	dir := t.TempDir()
	topology := filepath.Join(dir, "structure.gro")
	trajectory := filepath.Join(dir, "traj.xtc")
	if err := os.WriteFile(topology, []byte("topology"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trajectory, []byte("trajectory"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := store.Ingest(IngestOptions{
		ID:          "run1",
		Name:        "Run 1",
		Description: "short test trajectory",
		Source:      "doi:10.test/example",
		Topology:    topology,
		Trajectory:  trajectory,
		Stride:      10,
		AtomSubset:  "not water",
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Metadata.ID != "run1" {
		t.Fatalf("unexpected manifest id %q", manifest.Metadata.ID)
	}
	if manifest.Inputs.Topology.Path != "topology/run1.gro" {
		t.Fatalf("unexpected topology path %q", manifest.Inputs.Topology.Path)
	}
	if manifest.Inputs.Trajectories[0].Path != "trajectory/run1.xtc" {
		t.Fatalf("unexpected trajectory path %q", manifest.Inputs.Trajectories[0].Path)
	}
	if manifest.Inputs.Trajectories[0].SHA256 == "" {
		t.Fatal("expected trajectory sha256")
	}

	loaded, err := store.LoadDataset("run1")
	if err != nil {
		t.Fatal(err)
	}
	report := store.CheckDataset(loaded)
	for _, file := range report.Files {
		if !file.Exists || file.Error != "" {
			t.Fatalf("file check failed: %#v", file)
		}
	}

	var index []trajectoryIndexEntry
	data, err := os.ReadFile(filepath.Join(store.Root, "trajectory_index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	if len(index) != 1 || index[0].ID != "run1" || index[0].Name != "Run 1" {
		t.Fatalf("unexpected trajectory index: %#v", index)
	}
}

func TestStoreInitWritesVersionMetadata(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	metadata, err := store.LoadMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Version != StoreVersion || metadata.ManifestVersion != ManifestVersion || metadata.CreatedAt == "" {
		t.Fatalf("unexpected store metadata: %#v", metadata)
	}
	report := store.Doctor()
	if !report.OK || report.Version != StoreVersion || len(report.Checks) == 0 {
		t.Fatalf("unexpected store doctor report: %#v", report)
	}
}

func TestStoreDoctorReportsMissingMetadataMigration(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store.Root, DatasetsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	report := store.Doctor()
	if report.OK {
		t.Fatalf("expected incomplete store to fail doctor: %#v", report)
	}
	found := false
	for _, migration := range report.Migrations {
		if migration.ID == "create-store-metadata" && migration.To == StoreVersion {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected metadata migration hint: %#v", report.Migrations)
	}
}

func TestIngestProtectsExistingDataset(t *testing.T) {
	dir := t.TempDir()
	topology := filepath.Join(dir, "structure.gro")
	trajectory := filepath.Join(dir, "traj.xtc")
	if err := os.WriteFile(topology, []byte("topology"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trajectory, []byte("trajectory"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	opts := IngestOptions{ID: "run1", Topology: topology, Trajectory: trajectory}
	if _, err := store.Ingest(opts); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Ingest(opts); err == nil {
		t.Fatal("expected duplicate ingest to fail")
	}
	opts.Force = true
	if _, err := store.Ingest(opts); err != nil {
		t.Fatal(err)
	}
}

func TestIngestDerivesDatasetID(t *testing.T) {
	dir := t.TempDir()
	topology := filepath.Join(dir, "structure.gro")
	trajectory := filepath.Join(dir, "Replica 1.xtc")
	if err := os.WriteFile(topology, []byte("topology"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trajectory, []byte("trajectory"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := store.Ingest(IngestOptions{Topology: topology, Trajectory: trajectory})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Metadata.ID != "Replica-1" {
		t.Fatalf("derived id = %q", manifest.Metadata.ID)
	}
}

func TestFramePointsUseIrregularProbeTimes(t *testing.T) {
	points := framePoints(gromacs.TrajectoryProbe{
		FrameCount: 4,
		TimeStart:  0,
		TimeEnd:    7,
		TimeStep:   7.0 / 3.0,
		FrameTimes: []float64{0, 0.5, 2.25, 7},
	})
	if len(points) != 4 {
		t.Fatalf("points = %#v", points)
	}
	if points[1].Time != 0.5 || points[2].Time != 2.25 {
		t.Fatalf("irregular times were not preserved: %#v", points)
	}
}

func TestIngestDownloadsRemoteInputs(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/structure.gro":
			_, _ = w.Write([]byte("topology"))
		case "/traj.xtc":
			_, _ = w.Write([]byte("trajectory"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := store.Ingest(IngestOptions{
		ID:            "remote-run",
		TopologyURL:   server.URL + "/structure.gro",
		TrajectoryURL: server.URL + "/traj.xtc",
		Cache:         filepath.Join(dir, "cache"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Inputs.Topology.URL == "" || manifest.Inputs.Trajectories[0].URL == "" {
		t.Fatalf("expected original URLs in manifest: %#v", manifest.Inputs)
	}
	if _, err := os.Stat(filepath.Join(store.Root, manifest.Inputs.Topology.Path)); err != nil {
		t.Fatal(err)
	}
}

func TestIngestRedirectCannotBypassAllowedHosts(t *testing.T) {
	dir := t.TempDir()
	// An allowed host that maliciously (or via misconfiguration) redirects the
	// download to an internal metadata endpoint. The redirect target host is
	// not in the allowlist, so the download must be refused before the client
	// ever connects to it.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer server.Close()
	allowedHost := mustHostname(t, server.URL)

	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Ingest(IngestOptions{
		ID:           "remote-run",
		TopologyURL:  server.URL + "/structure.gro",
		Trajectory:   filepath.Join(dir, "unused.xtc"),
		Cache:        filepath.Join(dir, "cache"),
		AllowedHosts: []string{allowedHost},
	})
	if err == nil {
		t.Fatal("expected redirect to a disallowed host to be rejected")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected host allowlist error, got: %v", err)
	}
}

func TestIngestAllowedHostsPermitsMatchingHost(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/structure.gro":
			_, _ = w.Write([]byte("topology"))
		case "/traj.xtc":
			_, _ = w.Write([]byte("trajectory"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	allowedHost := mustHostname(t, server.URL)

	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Ingest(IngestOptions{
		ID:            "remote-run",
		TopologyURL:   server.URL + "/structure.gro",
		TrajectoryURL: server.URL + "/traj.xtc",
		Cache:         filepath.Join(dir, "cache"),
		AllowedHosts:  []string{allowedHost},
	}); err != nil {
		t.Fatalf("allowed host download should succeed: %v", err)
	}
}

func mustHostname(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Hostname()
}

func TestSaveSelectionAndResolveExpression(t *testing.T) {
	dir := t.TempDir()
	topology := filepath.Join(dir, "structure.gro")
	trajectory := filepath.Join(dir, "traj.xtc")
	if err := os.WriteFile(topology, []byte("topology"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trajectory, []byte("trajectory"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Ingest(IngestOptions{ID: "run1", Topology: topology, Trajectory: trajectory})
	if err != nil {
		t.Fatal(err)
	}
	m.Inputs.Trajectories[0].AtomCount = 4
	if err := WriteManifestFile(store.ManifestPath("run1"), m); err != nil {
		t.Fatal(err)
	}
	selection, err := store.SaveSelection("run1", Selection{ID: "first-two", Expression: "1-2", Kind: "atom-index"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.AtomCount != 2 {
		t.Fatalf("expected atom count 2, got %d", selection.AtomCount)
	}
	loaded, err := store.LoadDataset("run1")
	if err != nil {
		t.Fatal(err)
	}
	if ResolveSelectionExpression(loaded, "@first-two") != "1-2" {
		t.Fatalf("selection did not resolve: %#v", loaded.Selections)
	}
	pythonSelection, err := ResolveSelectionForTarget(loaded, "first-two", "python", 4)
	if err != nil {
		t.Fatal(err)
	}
	if pythonSelection != "index 0 or index 1" {
		t.Fatalf("unexpected python selection %q", pythonSelection)
	}
	gromacsSelection, err := ResolveSelectionForTarget(loaded, "@first-two", "gromacs", 4)
	if err != nil {
		t.Fatal(err)
	}
	if gromacsSelection != "1-2" {
		t.Fatalf("unexpected gromacs selection %q", gromacsSelection)
	}
	mvsSelection, err := ResolveSelectionForTarget(loaded, "@first-two", "mvs", 4)
	if err != nil {
		t.Fatal(err)
	}
	if mvsSelection != "atom-index:1,atom-index:2" {
		t.Fatalf("unexpected mvs selection %q", mvsSelection)
	}
}

func TestParseAtomSelectionAcceptsCLIDSL(t *testing.T) {
	for _, value := range []string{"1-2", "atom:1-2", "atoms 1,3", "index:2"} {
		atoms, err := ParseAtomSelection(value, 3)
		if err != nil {
			t.Fatalf("%s: %v", value, err)
		}
		if len(atoms) == 0 {
			t.Fatalf("%s matched no atoms", value)
		}
	}
}

func TestManifestValidationRequiresVersionAndInputs(t *testing.T) {
	m := Manifest{Version: ManifestVersion, Metadata: Metadata{ID: "run1"}}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestFrameChunkCodecsRoundTrip(t *testing.T) {
	chunk := FrameChunkData{
		DatasetID: "run1",
		Chunk:     0,
		Start:     0,
		Stop:      2,
		Frames: []Frame{
			{
				Backend:        "test",
				Frame:          0,
				Time:           0,
				TimeUnit:       "ps",
				CoordinateUnit: "nm",
				UnitCell:       [][3]float32{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}},
				Coordinates:    [][3]float32{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}},
			},
			{
				Backend:        "test",
				Frame:          1,
				Time:           1.5,
				TimeUnit:       "ps",
				CoordinateUnit: "nm",
				UnitCell:       [][3]float32{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}},
				Coordinates:    [][3]float32{{0.2, 0.2, 0.3}, {0.4, 0.6, 0.6}},
			},
		},
	}
	for _, encoding := range []string{"json", "bin", "bin-zstd"} {
		chunk.Encoding = encoding
		raw, storedEncoding, err := EncodeFrameChunk(chunk)
		if err != nil {
			t.Fatalf("%s encode: %v", encoding, err)
		}
		decoded, err := DecodeFrameChunk(raw, storedEncoding)
		if err != nil {
			t.Fatalf("%s decode: %v", encoding, err)
		}
		if decoded.DatasetID != "run1" || decoded.Chunk != 0 || len(decoded.Frames) != 2 {
			t.Fatalf("%s decoded unexpected chunk: %#v", encoding, decoded)
		}
		if decoded.Frames[1].Frame != 1 || decoded.Frames[1].Coordinates[1] != [3]float32{0.4, 0.6, 0.6} {
			t.Fatalf("%s decoded unexpected frame: %#v", encoding, decoded.Frames[1])
		}
	}
}

func TestPublishSessionUpdatesManifestAndIndex(t *testing.T) {
	dir := t.TempDir()
	topology := filepath.Join(dir, "structure.gro")
	trajectory := filepath.Join(dir, "traj.xtc")
	session := filepath.Join(dir, "session.molj")
	if err := os.WriteFile(topology, []byte("topology"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trajectory, []byte("trajectory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Ingest(IngestOptions{ID: "run1", Topology: topology, Trajectory: trajectory}); err != nil {
		t.Fatal(err)
	}
	ref, err := store.PublishSession(SessionOptions{
		ID:        "run1-session",
		DatasetID: "run1",
		Name:      "Run 1 session",
		File:      session,
		IsSticky:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Path != "session/run1-session.molj" {
		t.Fatalf("unexpected session path %q", ref.Path)
	}
	manifest, err := store.LoadDataset("run1")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Visualization.Sessions) != 1 {
		t.Fatalf("expected one session ref, got %#v", manifest.Visualization.Sessions)
	}
	var index []sessionIndexEntry
	data, err := os.ReadFile(filepath.Join(store.Root, "session_index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	if len(index) != 1 || index[0].ID != "run1-session" || !index[0].IsSticky {
		t.Fatalf("unexpected session index: %#v", index)
	}
	sessions, err := store.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "run1-session" || sessions[0].Path != "session/run1-session.molj" {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}
}

func TestPublishStaticSkipsAppleDoubleFiles(t *testing.T) {
	dir := t.TempDir()
	topology := filepath.Join(dir, "structure.gro")
	trajectory := filepath.Join(dir, "traj.xtc")
	if err := os.WriteFile(topology, []byte("topology"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trajectory, []byte("trajectory"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Ingest(IngestOptions{ID: "run1", Topology: topology, Trajectory: trajectory}); err != nil {
		t.Fatal(err)
	}
	// Simulate a macOS AppleDouble sidecar next to a real store file, as would
	// appear when the store lives on a non-native filesystem (exFAT/SMB).
	appleDouble := filepath.Join(store.Root, TopologyDir, "._run1.gro")
	if err := os.WriteFile(appleDouble, []byte{0x00, 0x05, 0x16, 0x07}, 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "static")
	report, err := store.PublishStatic(out, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Files {
		if strings.Contains(f, "._") {
			t.Fatalf("AppleDouble file was published: %q", f)
		}
	}
	if _, err := os.Stat(filepath.Join(out, TopologyDir, "._run1.gro")); !os.IsNotExist(err) {
		t.Fatalf("AppleDouble file leaked into published output (stat err=%v)", err)
	}
}

func TestPackDatasetWritesArchive(t *testing.T) {
	dir := t.TempDir()
	topology := filepath.Join(dir, "structure.gro")
	trajectory := filepath.Join(dir, "traj.xtc")
	if err := os.WriteFile(topology, []byte("topology"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trajectory, []byte("trajectory"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Ingest(IngestOptions{ID: "run1", Topology: topology, Trajectory: trajectory}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "run1.mdsrvx")
	report, err := store.PackDataset("run1", out, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Path != out {
		t.Fatalf("unexpected report path %q", report.Path)
	}
	reader, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	names := map[string]bool{}
	for _, file := range reader.File {
		names[file.Name] = true
	}
	for _, want := range []string{"index.yaml", "datasets/run1.yaml", "topology/run1.gro", "trajectory/run1.xtc", "trajectory_index.json", "session_index.json"} {
		if !names[want] {
			t.Fatalf("archive missing %s; got %#v", want, names)
		}
	}
	unpacked, err := OpenStore(filepath.Join(dir, "unpacked"))
	if err != nil {
		t.Fatal(err)
	}
	unpackReport, err := unpacked.UnpackArchive(out, false)
	if err != nil {
		t.Fatal(err)
	}
	if unpackReport.ID != "run1" {
		t.Fatalf("unexpected unpack id %q", unpackReport.ID)
	}
	if _, err := unpacked.LoadDataset("run1"); err != nil {
		t.Fatal(err)
	}
	if doctor := unpacked.Doctor(); !doctor.OK {
		t.Fatalf("unpacked archive is not a healthy store: %#v", doctor)
	}
}

func TestLoadBatchFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.jsonl")
	data := []byte(`{"id":"run1","topology_url":"https://example.org/structure.gro","trajectory_url":"https://example.org/traj.xtc","cache":".cache"}` + "\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	jobs, err := LoadBatchFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != "run1" {
		t.Fatalf("unexpected jobs: %#v", jobs)
	}
	if jobs[0].TopologyURL == "" || jobs[0].TrajectoryURL == "" || jobs[0].IngestOptions(false).Cache != ".cache" {
		t.Fatalf("unexpected remote batch job: %#v", jobs[0])
	}
}

func TestUnpackArchiveRejectsEscapingPath(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad.mdsrvx")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("bad")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UnpackArchive(archive, false); err == nil {
		t.Fatal("expected unpack to reject escaping path")
	}
}

// TestUnpackArchiveRejectsEscapingManifestID ensures a crafted archive cannot
// write its manifest outside the store via a traversal in metadata.id.
func TestUnpackArchiveRejectsEscapingManifestID(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad-id.mdsrvx")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("index.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("version: mdsrv.store/v1\nmetadata:\n  id: ../../../escaped-manifest\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UnpackArchive(archive, false); err == nil {
		t.Fatal("expected unpack to reject escaping manifest id")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped-manifest.yaml")); !os.IsNotExist(err) {
		t.Fatalf("manifest was written outside the store: err=%v", err)
	}
}
