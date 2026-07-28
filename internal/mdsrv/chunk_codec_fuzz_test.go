package mdsrv

import "testing"

// FuzzDecodeFrameChunk asserts that decoding never panics regardless of input.
// The frame decoders parse length-prefixed binary (optionally zstd-compressed)
// that can originate from an untrusted upload, so a panic here would be a
// denial-of-service. Returning an error is fine; crashing is not.
func FuzzDecodeFrameChunk(f *testing.F) {
	for _, encoding := range []string{
		FrameChunkEncodingJSON,
		FrameChunkEncodingBinary,
		FrameChunkEncodingBinaryZstd,
	} {
		if raw, resolved, err := EncodeFrameChunk(syntheticFrameChunk(3, 5, encoding)); err == nil {
			f.Add(raw, resolved)
		}
	}
	// A few degenerate seeds that exercise the header-parsing branches.
	f.Add([]byte("MDSC"), FrameChunkEncodingBinary)
	f.Add([]byte("MDSF\x01\x00"), FrameChunkEncodingBinary)
	f.Add([]byte("{"), FrameChunkEncodingJSON)
	f.Add([]byte{}, "")

	f.Fuzz(func(t *testing.T, raw []byte, encoding string) {
		// Must not panic. Errors are the expected outcome for malformed input.
		if data, err := DecodeFrameChunk(raw, encoding); err == nil {
			// A successful decode must round-trip without panicking either.
			_, _, _ = EncodeFrameChunk(data)
		}
		// DecodeFrameBinary is reachable directly from a single-frame payload.
		_, _ = DecodeFrameBinary(raw)
	})
}
