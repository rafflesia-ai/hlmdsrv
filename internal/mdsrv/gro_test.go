package mdsrv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGROFrame(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.gro")
	if err := os.WriteFile(path, []byte(`Demo t= 2.5
    2
    1MOL     C1    1   0.100   0.200   0.300
    1MOL     O1    2   0.400   0.500   0.600
   1.00000   2.00000   3.00000
`), 0o644); err != nil {
		t.Fatal(err)
	}
	frame, err := ParseGROFrame(path, 7)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Frame != 7 || frame.Time != 2.5 {
		t.Fatalf("unexpected frame metadata: %#v", frame)
	}
	if len(frame.Coordinates) != 2 {
		t.Fatalf("expected 2 coordinates, got %d", len(frame.Coordinates))
	}
	if frame.Coordinates[1] != [3]float32{0.4, 0.5, 0.6} {
		t.Fatalf("unexpected coordinate: %#v", frame.Coordinates[1])
	}
}

// TestParseGROFrameWithVelocities ensures atom positions are read even when the
// trajectory carries velocities (GROMACS writes 3 position + 3 velocity values
// per atom). The parser must return positions, not the trailing velocities.
func TestParseGROFrameWithVelocities(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vel.gro")
	if err := os.WriteFile(path, []byte(`Demo t= 1.0
    2
    1MOL     C1    1   0.100   0.200   0.300  1.1000 -2.2000  3.3000
    1MOL     O1    2   0.400   0.500   0.600 -4.4000  5.5000 -6.6000
   1.00000   2.00000   3.00000
`), 0o644); err != nil {
		t.Fatal(err)
	}
	frame, err := ParseGROFrame(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Coordinates[0] != [3]float32{0.1, 0.2, 0.3} {
		t.Fatalf("atom 0: got %#v, want positions {0.1 0.2 0.3} (not velocities)", frame.Coordinates[0])
	}
	if frame.Coordinates[1] != [3]float32{0.4, 0.5, 0.6} {
		t.Fatalf("atom 1: got %#v, want positions {0.4 0.5 0.6} (not velocities)", frame.Coordinates[1])
	}
}

// TestParseGROFrameRejectsNegativeAtomCount ensures a negative declared atom
// count returns an error instead of panicking on make with a negative capacity.
func TestParseGROFrameRejectsNegativeAtomCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "neg.gro")
	if err := os.WriteFile(path, []byte("title t=0.0\n-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseGROFrame(path, 0); err == nil {
		t.Fatal("expected negative atom count to be rejected")
	}
}

// TestParseGROFrameHugeAtomCountDoesNotOOM ensures a crafted huge atom count in
// a tiny file fails cleanly (short-file error) rather than pre-allocating gigabytes.
func TestParseGROFrameHugeAtomCountDoesNotOOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.gro")
	if err := os.WriteFile(path, []byte("title t=0.0\n2000000000\n0.1 0.2 0.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseGROFrame(path, 0); err == nil {
		t.Fatal("expected short file with huge atom count to error")
	}
}
