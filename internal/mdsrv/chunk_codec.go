package mdsrv

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	FrameChunkEncodingJSON       = "mdsrv-frames-json-v1"
	FrameChunkEncodingBinary     = "mdsrv-frames-bin-v1"
	FrameChunkEncodingBinaryZstd = "mdsrv-frames-bin-zstd-v1"
)

// maxDecodedChunkBytes bounds the output of zstd chunk decompression. Real
// chunks are far smaller (the encode side caps chunk bytes at ~100 MiB); this
// ceiling only exists to defeat a decompression bomb from a crafted archive.
const maxDecodedChunkBytes = 1 << 30 // 1 GiB

type FrameChunkFile struct {
	Path        string `json:"path"`
	Encoding    string `json:"encoding"`
	ContentType string `json:"content_type"`
	Bytes       []byte `json:"-"`
}

type frameChunkBinaryMeta struct {
	DatasetID string `json:"dataset_id"`
	Chunk     int    `json:"chunk"`
	Start     int    `json:"start"`
	Stop      int    `json:"stop"`
	Encoding  string `json:"encoding"`
}

func NormalizeFrameChunkEncoding(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "json", "application/json", FrameChunkEncodingJSON:
		return FrameChunkEncodingJSON, nil
	case "bin", "binary", "application/vnd.mdsrv.frames+bin", FrameChunkEncodingBinary:
		return FrameChunkEncodingBinary, nil
	case "zstd", "bin-zstd", "binary-zstd", "application/vnd.mdsrv.frames+zstd", FrameChunkEncodingBinaryZstd:
		return FrameChunkEncodingBinaryZstd, nil
	default:
		return "", fmt.Errorf("unsupported frame chunk encoding %q", value)
	}
}

func FrameChunkExtension(encoding string) string {
	normalized, err := NormalizeFrameChunkEncoding(encoding)
	if err != nil {
		return ".json"
	}
	switch normalized {
	case FrameChunkEncodingBinary:
		return ".bin"
	case FrameChunkEncodingBinaryZstd:
		return ".bin.zst"
	default:
		return ".json"
	}
}

func FrameChunkContentType(encoding string) string {
	normalized, err := NormalizeFrameChunkEncoding(encoding)
	if err != nil {
		return "application/octet-stream"
	}
	switch normalized {
	case FrameChunkEncodingBinary:
		return "application/vnd.mdsrv.frames+bin"
	case FrameChunkEncodingBinaryZstd:
		return "application/vnd.mdsrv.frames+zstd"
	default:
		return "application/json"
	}
}

func EncodeFrameChunk(data FrameChunkData) ([]byte, string, error) {
	encoding, err := NormalizeFrameChunkEncoding(data.Encoding)
	if err != nil {
		return nil, "", err
	}
	data.Encoding = encoding
	switch encoding {
	case FrameChunkEncodingJSON:
		encoded, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return nil, "", err
		}
		return append(encoded, '\n'), encoding, nil
	case FrameChunkEncodingBinary:
		encoded, err := encodeFrameChunkBinary(data)
		return encoded, encoding, err
	case FrameChunkEncodingBinaryZstd:
		binaryChunk, err := encodeFrameChunkBinary(data)
		if err != nil {
			return nil, "", err
		}
		encoder, err := zstd.NewWriter(nil)
		if err != nil {
			return nil, "", err
		}
		defer encoder.Close()
		return encoder.EncodeAll(binaryChunk, nil), encoding, nil
	default:
		return nil, "", fmt.Errorf("unsupported frame chunk encoding %q", encoding)
	}
}

func DecodeFrameChunk(raw []byte, encoding string) (FrameChunkData, error) {
	normalized, err := NormalizeFrameChunkEncoding(firstNonEmpty(encoding, inferFrameChunkEncoding(raw, "")))
	if err != nil {
		return FrameChunkData{}, err
	}
	switch normalized {
	case FrameChunkEncodingJSON:
		var data FrameChunkData
		if err := json.Unmarshal(raw, &data); err != nil {
			return FrameChunkData{}, err
		}
		if data.Encoding == "" {
			data.Encoding = FrameChunkEncodingJSON
		}
		return data, nil
	case FrameChunkEncodingBinary:
		return decodeFrameChunkBinary(raw, normalized)
	case FrameChunkEncodingBinaryZstd:
		// Bound the decoded size so a crafted chunk cannot act as a
		// decompression bomb (the library default is 64 GiB).
		decoder, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(maxDecodedChunkBytes))
		if err != nil {
			return FrameChunkData{}, err
		}
		defer decoder.Close()
		decoded, err := decoder.DecodeAll(raw, nil)
		if err != nil {
			return FrameChunkData{}, err
		}
		return decodeFrameChunkBinary(decoded, normalized)
	default:
		return FrameChunkData{}, fmt.Errorf("unsupported frame chunk encoding %q", normalized)
	}
}

func DecodeFrameBinary(raw []byte) (Frame, error) {
	reader := bytes.NewReader(raw)
	magic := make([]byte, 4)
	if _, err := io.ReadFull(reader, magic); err != nil {
		return Frame{}, err
	}
	if string(magic) != "MDSF" {
		return Frame{}, fmt.Errorf("invalid frame magic %q", string(magic))
	}
	var version uint16
	if err := binary.Read(reader, binary.LittleEndian, &version); err != nil {
		return Frame{}, err
	}
	if version != 1 {
		return Frame{}, fmt.Errorf("unsupported frame binary version %d", version)
	}
	var frameIndex uint32
	var timeValue float64
	var atomCount uint32
	if err := binary.Read(reader, binary.LittleEndian, &frameIndex); err != nil {
		return Frame{}, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &timeValue); err != nil {
		return Frame{}, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &atomCount); err != nil {
		return Frame{}, err
	}
	unitCell := make([][3]float32, 3)
	for i := range unitCell {
		for j := range unitCell[i] {
			if err := binary.Read(reader, binary.LittleEndian, &unitCell[i][j]); err != nil {
				return Frame{}, err
			}
		}
	}
	// Each coordinate triple is 3 float32 (12 bytes); reject counts that exceed
	// the bytes actually present so a crafted atomCount cannot force a huge
	// allocation before the per-coordinate reads fail.
	if int64(atomCount)*12 > int64(reader.Len()) {
		return Frame{}, fmt.Errorf("atom count %d exceeds available bytes", atomCount)
	}
	coordinates := make([][3]float32, atomCount)
	for i := range coordinates {
		for j := range coordinates[i] {
			if err := binary.Read(reader, binary.LittleEndian, &coordinates[i][j]); err != nil {
				return Frame{}, err
			}
			if math.IsNaN(float64(coordinates[i][j])) || math.IsInf(float64(coordinates[i][j]), 0) {
				return Frame{}, errors.New("frame contains non-finite coordinate")
			}
		}
	}
	if reader.Len() != 0 {
		return Frame{}, fmt.Errorf("frame binary has %d trailing bytes", reader.Len())
	}
	return Frame{
		Backend:        "chunk",
		Frame:          int(frameIndex),
		Time:           timeValue,
		TimeUnit:       "ps",
		CoordinateUnit: "nm",
		UnitCell:       unitCell,
		Coordinates:    coordinates,
	}, nil
}

func inferFrameChunkEncoding(raw []byte, path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if strings.HasSuffix(strings.ToLower(path), ".bin.zst") || ext == ".zst" {
		return FrameChunkEncodingBinaryZstd
	}
	if ext == ".bin" {
		return FrameChunkEncodingBinary
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.HasPrefix(trimmed, []byte("{")) {
		return FrameChunkEncodingJSON
	}
	if bytes.HasPrefix(raw, []byte("MDSC")) {
		return FrameChunkEncodingBinary
	}
	return FrameChunkEncodingJSON
}

func encodeFrameChunkBinary(data FrameChunkData) ([]byte, error) {
	meta := frameChunkBinaryMeta{
		DatasetID: data.DatasetID,
		Chunk:     data.Chunk,
		Start:     data.Start,
		Stop:      data.Stop,
		Encoding:  data.Encoding,
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("MDSC")
	if err := binary.Write(&buf, binary.LittleEndian, uint16(1)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(metaBytes))); err != nil {
		return nil, err
	}
	if _, err := buf.Write(metaBytes); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(data.Frames))); err != nil {
		return nil, err
	}
	for _, frame := range data.Frames {
		frameBytes, err := EncodeFrameBinary(frame)
		if err != nil {
			return nil, err
		}
		if err := binary.Write(&buf, binary.LittleEndian, uint32(len(frameBytes))); err != nil {
			return nil, err
		}
		if _, err := buf.Write(frameBytes); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func decodeFrameChunkBinary(raw []byte, outerEncoding string) (FrameChunkData, error) {
	reader := bytes.NewReader(raw)
	magic := make([]byte, 4)
	if _, err := io.ReadFull(reader, magic); err != nil {
		return FrameChunkData{}, err
	}
	if string(magic) != "MDSC" {
		return FrameChunkData{}, fmt.Errorf("invalid frame chunk magic %q", string(magic))
	}
	var version uint16
	if err := binary.Read(reader, binary.LittleEndian, &version); err != nil {
		return FrameChunkData{}, err
	}
	if version != 1 {
		return FrameChunkData{}, fmt.Errorf("unsupported frame chunk binary version %d", version)
	}
	var metaLength uint32
	if err := binary.Read(reader, binary.LittleEndian, &metaLength); err != nil {
		return FrameChunkData{}, err
	}
	if metaLength == 0 || metaLength > 1<<20 {
		return FrameChunkData{}, fmt.Errorf("invalid frame chunk metadata length %d", metaLength)
	}
	metaBytes := make([]byte, metaLength)
	if _, err := io.ReadFull(reader, metaBytes); err != nil {
		return FrameChunkData{}, err
	}
	var meta frameChunkBinaryMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return FrameChunkData{}, err
	}
	var frameCount uint32
	if err := binary.Read(reader, binary.LittleEndian, &frameCount); err != nil {
		return FrameChunkData{}, err
	}
	// Each frame is prefixed by a 4-byte length, so a valid frameCount cannot
	// exceed the remaining bytes divided by four. Guard before allocating so a
	// crafted count cannot force a huge up-front allocation.
	if int64(frameCount) > int64(reader.Len())/4 {
		return FrameChunkData{}, fmt.Errorf("frame count %d exceeds available bytes", frameCount)
	}
	frames := make([]Frame, 0, frameCount)
	for i := uint32(0); i < frameCount; i++ {
		var frameLength uint32
		if err := binary.Read(reader, binary.LittleEndian, &frameLength); err != nil {
			return FrameChunkData{}, err
		}
		if frameLength == 0 || int(frameLength) > reader.Len() {
			return FrameChunkData{}, fmt.Errorf("invalid frame %d length %d", i, frameLength)
		}
		frameBytes := make([]byte, frameLength)
		if _, err := io.ReadFull(reader, frameBytes); err != nil {
			return FrameChunkData{}, err
		}
		frame, err := DecodeFrameBinary(frameBytes)
		if err != nil {
			return FrameChunkData{}, fmt.Errorf("decode frame %d: %w", i, err)
		}
		frames = append(frames, frame)
	}
	if reader.Len() != 0 {
		return FrameChunkData{}, fmt.Errorf("frame chunk binary has %d trailing bytes", reader.Len())
	}
	return FrameChunkData{
		DatasetID: meta.DatasetID,
		Chunk:     meta.Chunk,
		Start:     meta.Start,
		Stop:      meta.Stop,
		Encoding:  outerEncoding,
		Frames:    frames,
	}, nil
}
