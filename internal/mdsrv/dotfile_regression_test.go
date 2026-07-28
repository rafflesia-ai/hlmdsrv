package mdsrv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Finding #5: ListDatasets globbed datasets/*.yaml and returned an error on the
// first match it could not parse. macOS writes an AppleDouble "._<name>" sibling
// for extended attributes on every non-native filesystem (exFAT, FAT, SMB, USB) —
// where multi-GB trajectory stores tend to live — so a valid store became
// unlistable and unpublishable, blaming a manifest the user never created.
func TestListDatasetsSkipsAppleDoubleSidecars(t *testing.T) {
	store := newStoreWithDataset(t)

	// AppleDouble sidecars are binary; yaml decoding fails on the control bytes.
	sidecar := filepath.Join(store.Root, DatasetsDir, "._demo.yaml")
	if err := os.WriteFile(sidecar, []byte("\x00\x05\x16\x07binary xattr blob"), 0o644); err != nil {
		t.Fatal(err)
	}

	summaries, err := store.ListDatasets()
	if err != nil {
		t.Fatalf("an AppleDouble sidecar must not break the listing: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != "demo" {
		t.Fatalf("got %d summaries (%+v), want just the real dataset", len(summaries), summaries)
	}
}

// The skip keys on the leading dot generally, not on the "._" prefix: the store
// never writes a dotfile, so none of them are ours to parse.
func TestListDatasetsSkipsAnyDotfile(t *testing.T) {
	store := newStoreWithDataset(t)
	hidden := filepath.Join(store.Root, DatasetsDir, ".hidden.yaml")
	if err := os.WriteFile(hidden, []byte("\x00not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListDatasets(); err != nil {
		t.Fatalf("a stray dotfile must not break the listing: %v", err)
	}
}

// Guard against over-suppression: a genuinely corrupt manifest must still fail,
// or the fix would hide real store damage.
func TestListDatasetsStillFailsOnACorruptManifest(t *testing.T) {
	store := newStoreWithDataset(t)
	broken := filepath.Join(store.Root, DatasetsDir, "broken.yaml")
	if err := os.WriteFile(broken, []byte("\x00not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListDatasets(); err == nil {
		t.Fatal("a corrupt (non-dotfile) manifest must still be reported")
	}
}

// Ninth pass: the manifest path is derived from the id, so on a case-insensitive
// filesystem (macOS, Windows, exFAT — where large trajectory stores live)
// `dataset inspect alpha` opened Alpha.yaml and returned a dataset whose id was
// "Alpha", while the same store on Linux reported no such dataset. The requested
// id is now compared against the loaded one.
//
// The mismatch is constructed explicitly rather than by relying on the host
// filesystem's case behavior, so the test is deterministic everywhere.
func TestLoadDatasetRejectsAnIDThatDoesNotMatchTheManifest(t *testing.T) {
	store := newStoreWithDataset(t) // holds a dataset whose metadata id is "demo"

	// Write the same manifest under a second name; its metadata still says "demo".
	original, err := os.ReadFile(store.ManifestPath("demo"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root, DatasetsDir, "other.yaml"), original, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := store.LoadDataset("other"); err == nil {
		t.Fatal("loading by an id the manifest does not carry must fail")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should read as not-found, got: %v", err)
	}

	// The real id still loads.
	if _, err := store.LoadDataset("demo"); err != nil {
		t.Errorf("the matching id must still load: %v", err)
	}
}

func TestIsHiddenSidecar(t *testing.T) {
	for path, want := range map[string]bool{
		"/store/datasets/._demo.yaml": true,
		"/store/datasets/.hidden":     true,
		"/store/datasets/demo.yaml":   false,
		"/store/datasets/a._b.yaml":   false,
	} {
		if got := isHiddenSidecar(path); got != want {
			t.Errorf("isHiddenSidecar(%q) = %v, want %v", path, got, want)
		}
	}
}

// newStoreWithDataset builds a store holding exactly one valid dataset, written
// through the store's own Ingest path so the fixture cannot drift from the real
// on-disk shape.
func newStoreWithDataset(t *testing.T) Store {
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
	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Ingest(IngestOptions{ID: "demo", Topology: topology, Trajectory: trajectory}); err != nil {
		t.Fatal(err)
	}
	return store
}
