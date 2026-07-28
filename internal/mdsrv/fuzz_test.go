package mdsrv

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzDecodeFrameBinary asserts the MDSF frame decoder never panics.
func FuzzDecodeFrameBinary(f *testing.F) {
	if raw, err := EncodeFrameBinary(Frame{
		UnitCell:    make([][3]float32, 3),
		Coordinates: [][3]float32{{1, 2, 3}},
	}); err == nil {
		f.Add(raw)
	}
	f.Add([]byte("MDSF"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = DecodeFrameBinary(raw)
	})
}

// FuzzDecodeManifest asserts the manifest decoder never panics on arbitrary
// bytes — a manifest is read from an untrusted .mdsrvx archive during unpack.
func FuzzDecodeManifest(f *testing.F) {
	f.Add([]byte("version: mdsrv.store/v1\nmetadata:\n  id: run1\n"))
	f.Add([]byte(`{"version":"mdsrv.store/v1","metadata":{"id":"run1"}}`))
	f.Add([]byte("{"))
	f.Add([]byte("!!!"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeManifest(data, "fuzz.yaml")
	})
}

// FuzzFrameChunkRoundTrip is a property test: a chunk built from fuzzed frame /
// atom counts must survive encode -> decode unchanged across every encoding.
// This catches codec asymmetry, not just panics.
func FuzzFrameChunkRoundTrip(f *testing.F) {
	f.Add(3, 8)
	f.Add(0, 0)
	f.Add(1, 1)
	f.Add(16, 64)
	f.Fuzz(func(t *testing.T, frames, atoms int) {
		if frames < 0 || frames > 64 || atoms < 0 || atoms > 128 {
			t.Skip()
		}
		for _, enc := range []string{FrameChunkEncodingJSON, FrameChunkEncodingBinary, FrameChunkEncodingBinaryZstd} {
			original := syntheticFrameChunk(frames, atoms, enc)
			raw, resolved, err := EncodeFrameChunk(original)
			if err != nil {
				t.Fatalf("%s encode: %v", enc, err)
			}
			decoded, err := DecodeFrameChunk(raw, resolved)
			if err != nil {
				t.Fatalf("%s decode: %v", enc, err)
			}
			if len(decoded.Frames) != frames {
				t.Fatalf("%s frame count: got %d want %d", enc, len(decoded.Frames), frames)
			}
			for i := range decoded.Frames {
				if len(decoded.Frames[i].Coordinates) != atoms {
					t.Fatalf("%s frame %d atoms: got %d want %d", enc, i, len(decoded.Frames[i].Coordinates), atoms)
				}
				for a := range decoded.Frames[i].Coordinates {
					if decoded.Frames[i].Coordinates[a] != original.Frames[i].Coordinates[a] {
						t.Fatalf("%s frame %d atom %d: got %v want %v", enc, i, a,
							decoded.Frames[i].Coordinates[a], original.Frames[i].Coordinates[a])
					}
				}
			}
		}
	})
}

// FuzzParseAtomSelection asserts the selection DSL never panics; atomCount is
// bounded so "all" cannot legitimately allocate a huge slice.
func FuzzParseAtomSelection(f *testing.F) {
	f.Add("all")
	f.Add("1-10")
	f.Add("atom:1-5,7")
	f.Add("-1")
	f.Add("999999999-1")
	f.Fuzz(func(t *testing.T, value string) {
		_, _ = ParseAtomSelection(value, 128)
	})
}

// FuzzParseGROFrame asserts the .gro parser never panics on arbitrary content.
func FuzzParseGROFrame(f *testing.F) {
	f.Add("title t=0.0\n2\n    1MOL C1 1 0.1 0.2 0.3\n    1MOL O1 2 0.4 0.5 0.6\n1 2 3\n")
	f.Add("")
	f.Add("x\n-1\n")
	f.Add("x\n999999999\n0.1 0.2 0.3\n")
	// ParseGROFrame reads from a path, so the input has to reach the disk — but the
	// scratch directory is created ONCE here rather than per execution. Calling
	// t.TempDir() inside f.Fuzz makes a fresh directory for every input, which held
	// this target to ~700 execs/sec against 30,000+ for the others (a 40x loss of
	// exploration for the same wall time) and churned enough directories during a
	// 300s run to fill the volume, surfacing as a spurious failure.
	dir := f.TempDir()
	path := filepath.Join(dir, "f.gro")
	f.Fuzz(func(t *testing.T, content string) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Skip()
		}
		_, _ = ParseGROFrame(path, 0)
	})
}
