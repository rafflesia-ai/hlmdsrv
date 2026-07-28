package mdsrvcli

import "testing"

func TestDefaultGromacsFramePath(t *testing.T) {
	if got := defaultGromacsFramePath("/tmp/traj.xtc", 3, ""); got != "traj-frame-3.gro" {
		t.Fatalf("frame path = %q", got)
	}
	if got := defaultGromacsFramePath("/tmp/traj.xtc", -1, "4.5"); got != "traj-time-4.5.gro" {
		t.Fatalf("time path = %q", got)
	}
}
