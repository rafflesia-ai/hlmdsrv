package mdsrvcli

import "testing"

// TestOutputAnalysisPathMatchesFormat locks in that the default analysis trace
// filename extension reflects the requested format, so `--format json` does not
// write JSON content into a ".csv"-named file.
func TestOutputAnalysisPathMatchesFormat(t *testing.T) {
	cases := []struct {
		format string
		want   string
	}{
		{"", "traces/demo-rmsd.csv"},      // default is csv
		{"csv", "traces/demo-rmsd.csv"},   // explicit csv
		{"json", "traces/demo-rmsd.json"}, // json must not land in a .csv file
		{"JSON", "traces/demo-rmsd.json"}, // case-insensitive
	}
	for _, tc := range cases {
		if got := outputAnalysisPath("demo", "rmsd", "", tc.format); got != tc.want {
			t.Errorf("outputAnalysisPath(format=%q) = %q, want %q", tc.format, got, tc.want)
		}
	}

	// An explicit --out path is always honored verbatim, regardless of format.
	if got := outputAnalysisPath("demo", "rmsd", "custom/where.dat", "json"); got != "custom/where.dat" {
		t.Errorf("explicit out path not honored: %q", got)
	}
}
