package gromacs

import "testing"

// FuzzParseCheckOutput asserts the `gmx check` output parser never panics or
// hangs on crafted trajectory-derived output (frame counts, times, atom counts).
func FuzzParseCheckOutput(f *testing.F) {
	f.Add("Reading frame       0 time    0.000\n# Atoms  3\nLast frame          2 time    2.000\n")
	f.Add("")
	f.Add("Last frame  9223372036854775807 time 0")
	f.Add("Coords  2000000000  1.5\n")
	f.Fuzz(func(t *testing.T, s string) {
		_ = ParseCheckOutput(s)
	})
}
