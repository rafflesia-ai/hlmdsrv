package mdsrv

import "testing"

func BenchmarkFrameIndexLarge(b *testing.B) {
	const (
		frameCount = 250_000
		chunkSize  = 128
	)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		index := syntheticFrameIndex(frameCount, chunkSize)
		if index.FrameCount != frameCount || len(index.Chunks) == 0 {
			b.Fatalf("invalid synthetic index: %#v", index)
		}
	}
	b.ReportMetric(float64(frameCount), "frames/op")
	b.ReportMetric(float64((frameCount+chunkSize-1)/chunkSize), "chunks/op")
}

func BenchmarkFrameChunkEncodeJSONLarge(b *testing.B) {
	benchmarkFrameChunkEncode(b, FrameChunkEncodingJSON)
}

func BenchmarkFrameChunkEncodeBinaryLarge(b *testing.B) {
	benchmarkFrameChunkEncode(b, FrameChunkEncodingBinary)
}

func BenchmarkFrameChunkEncodeBinaryZstdLarge(b *testing.B) {
	benchmarkFrameChunkEncode(b, FrameChunkEncodingBinaryZstd)
}

func BenchmarkFrameChunkDecodeBinaryZstdLarge(b *testing.B) {
	chunk := syntheticFrameChunk(128, 1024, FrameChunkEncodingBinaryZstd)
	raw, encoding, err := EncodeFrameChunk(chunk)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoded, err := DecodeFrameChunk(raw, encoding)
		if err != nil {
			b.Fatal(err)
		}
		if len(decoded.Frames) != len(chunk.Frames) {
			b.Fatalf("decoded %d frames, want %d", len(decoded.Frames), len(chunk.Frames))
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(len(raw)), "encoded_bytes/op")
}

func benchmarkFrameChunkEncode(b *testing.B, encoding string) {
	chunk := syntheticFrameChunk(128, 1024, encoding)
	var encodedBytes int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		raw, storedEncoding, err := EncodeFrameChunk(chunk)
		if err != nil {
			b.Fatal(err)
		}
		if storedEncoding != encoding {
			b.Fatalf("stored encoding %q, want %q", storedEncoding, encoding)
		}
		encodedBytes = len(raw)
	}
	b.StopTimer()
	b.ReportMetric(float64(encodedBytes), "encoded_bytes/op")
}

func syntheticFrameIndex(frameCount, chunkSize int) FrameIndex {
	index := FrameIndex{
		DatasetID:       "bench",
		FrameCount:      frameCount,
		AtomCount:       1024,
		TimeStart:       0,
		TimeEnd:         float64(frameCount-1) * 10,
		TimeStep:        10,
		ChunkSizeFrames: chunkSize,
		Frames:          make([]FramePoint, 0, frameCount),
		Chunks:          make([]FrameChunk, 0, (frameCount+chunkSize-1)/chunkSize),
	}
	for i := 0; i < frameCount; i++ {
		index.Frames = append(index.Frames, FramePoint{Index: i, Time: float64(i) * 10})
	}
	for start, chunk := 0, 0; start < frameCount; start, chunk = start+chunkSize, chunk+1 {
		stop := start + chunkSize
		if stop > frameCount {
			stop = frameCount
		}
		index.Chunks = append(index.Chunks, FrameChunk{Index: chunk, Start: start, Stop: stop})
	}
	return index
}

func syntheticFrameChunk(frameCount, atomCount int, encoding string) FrameChunkData {
	chunk := FrameChunkData{
		DatasetID: "bench",
		Chunk:     0,
		Start:     0,
		Stop:      frameCount,
		Encoding:  encoding,
		Frames:    make([]Frame, 0, frameCount),
	}
	for frameIndex := 0; frameIndex < frameCount; frameIndex++ {
		frame := Frame{
			Backend:        "synthetic",
			Frame:          frameIndex,
			Time:           float64(frameIndex) * 10,
			TimeUnit:       "ps",
			CoordinateUnit: "nm",
			UnitCell:       [][3]float32{{10, 0, 0}, {0, 10, 0}, {0, 0, 10}},
			Coordinates:    make([][3]float32, atomCount),
		}
		for atom := 0; atom < atomCount; atom++ {
			base := float32(frameIndex*atomCount + atom)
			frame.Coordinates[atom] = [3]float32{
				base * 0.001,
				float32(atom%97) * 0.01,
				float32((frameIndex+atom)%53) * 0.02,
			}
		}
		chunk.Frames = append(chunk.Frames, frame)
	}
	return chunk
}
