package mdsrv

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// TestFrameChunkRoundTripAllEncodings proves the hardened decoders still accept
// legitimately encoded chunks across every supported encoding.
func TestFrameChunkRoundTripAllEncodings(t *testing.T) {
	for _, encoding := range []string{
		FrameChunkEncodingJSON,
		FrameChunkEncodingBinary,
		FrameChunkEncodingBinaryZstd,
	} {
		chunk := syntheticFrameChunk(4, 8, encoding)
		raw, resolved, err := EncodeFrameChunk(chunk)
		if err != nil {
			t.Fatalf("%s encode: %v", encoding, err)
		}
		decoded, err := DecodeFrameChunk(raw, resolved)
		if err != nil {
			t.Fatalf("%s decode: %v", encoding, err)
		}
		if len(decoded.Frames) != 4 || len(decoded.Frames[0].Coordinates) != 8 {
			t.Fatalf("%s round-trip mismatch: %d frames, %d atoms",
				encoding, len(decoded.Frames), len(decoded.Frames[0].Coordinates))
		}
	}
}

// TestDecodeFrameBinaryRejectsHugeAtomCount ensures a crafted atomCount cannot
// force a huge allocation before the coordinate reads fail.
func TestDecodeFrameBinaryRejectsHugeAtomCount(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("MDSF")
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))          // version
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))          // frame index
	_ = binary.Write(&buf, binary.LittleEndian, float64(0))         // time
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0xFFFFFFFF)) // atom count
	for i := 0; i < 9; i++ {                                        // unit cell
		_ = binary.Write(&buf, binary.LittleEndian, float32(0))
	}
	// No coordinate bytes follow.
	_, err := DecodeFrameBinary(buf.Bytes())
	if err == nil {
		t.Fatal("expected huge atom count to be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds available bytes") {
		t.Fatalf("expected allocation guard error, got: %v", err)
	}
}

// TestDecodeFrameChunkRejectsHugeFrameCount ensures a crafted frameCount cannot
// force a huge up-front slice allocation.
func TestDecodeFrameChunkRejectsHugeFrameCount(t *testing.T) {
	meta := []byte(`{"dataset_id":"x","chunk":0}`)
	var buf bytes.Buffer
	buf.WriteString("MDSC")
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))          // version
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(meta)))  // meta length
	buf.Write(meta)                                                 // meta
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0xFFFFFFFF)) // frame count
	// No frame bytes follow.
	_, err := DecodeFrameChunk(buf.Bytes(), FrameChunkEncodingBinary)
	if err == nil {
		t.Fatal("expected huge frame count to be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds available bytes") {
		t.Fatalf("expected allocation guard error, got: %v", err)
	}
}
