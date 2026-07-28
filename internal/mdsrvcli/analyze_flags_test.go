package mdsrvcli

import "testing"

// TestAnalyzeSelectionRouting locks in which analyses read the single --selection
// flag versus the multi-group --a/--b/--c/--d flags, so the --a fallback for
// single-group analyses (rmsd/rmsf/rgyr/sasa) stays consistent with routing.
func TestAnalyzeSelectionRouting(t *testing.T) {
	single := []string{"rmsd", "rmsf", "rgyr", "sasa"}
	for _, kind := range single {
		if !isSingleGroupAnalysis(kind) {
			t.Errorf("isSingleGroupAnalysis(%q) = false, want true", kind)
		}
		if got := selectionsFor(kind, &analyzeFlags{a: "atom:1"}); got != nil {
			t.Errorf("selectionsFor(%q) = %v, want nil (single-group uses --selection)", kind, got)
		}
	}

	multi := []string{"distance", "angle", "dihedral", "contacts"}
	for _, kind := range multi {
		if isSingleGroupAnalysis(kind) {
			t.Errorf("isSingleGroupAnalysis(%q) = true, want false", kind)
		}
		if got := selectionsFor(kind, &analyzeFlags{a: "atom:1", b: "atom:2"}); got["a"] != "atom:1" {
			t.Errorf("selectionsFor(%q) should route --a; got %v", kind, got)
		}
	}
}
