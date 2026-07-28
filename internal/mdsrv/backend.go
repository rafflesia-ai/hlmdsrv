package mdsrv

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed scripts/mdsrv_backend.py
var backendScript []byte

type Backend struct {
	Python    string
	Store     Store
	Preferred string
}

type BackendDoctor struct {
	Python     string `json:"python"`
	MDTraj     bool   `json:"mdtraj"`
	MDAnalysis bool   `json:"MDAnalysis"`
}

type TrajectoryInfo struct {
	Backend        string  `json:"backend"`
	Frames         int     `json:"frames"`
	Atoms          int     `json:"atoms"`
	TopologyAtoms  int     `json:"topology_atoms"`
	TimeUnit       string  `json:"time_unit"`
	CoordinateUnit string  `json:"coordinate_unit"`
	FirstTime      float64 `json:"first_time"`
	LastTime       float64 `json:"last_time"`
	HasUnitCell    bool    `json:"has_unit_cell"`
}

type Frame struct {
	Backend        string       `json:"backend"`
	Frame          int          `json:"frame"`
	Time           float64      `json:"time"`
	TimeUnit       string       `json:"time_unit"`
	CoordinateUnit string       `json:"coordinate_unit"`
	UnitCell       [][3]float32 `json:"unit_cell"`
	Coordinates    [][3]float32 `json:"coordinates"`
}

type Trace struct {
	Backend string       `json:"backend"`
	ID      string       `json:"id"`
	Type    string       `json:"type"`
	Unit    string       `json:"unit"`
	Values  []TraceValue `json:"values"`
}

type TraceValue struct {
	Frame int     `json:"frame"`
	Time  float64 `json:"time"`
	Value float64 `json:"value"`
}

type AnalysisRequest struct {
	ID             string            `json:"id"`
	Type           string            `json:"type"`
	Selection      string            `json:"selection,omitempty"`
	Selections     map[string]string `json:"selections,omitempty"`
	ReferenceFrame int               `json:"reference_frame,omitempty"`
	Cutoff         float64           `json:"cutoff,omitempty"`
	Output         string            `json:"output,omitempty"`
	Format         string            `json:"format,omitempty"`
}

func NewBackend(store Store) Backend {
	return Backend{Python: firstNonEmpty(os.Getenv("MDSRV_PYTHON"), "python3"), Store: store}
}

func (b Backend) Doctor(ctx context.Context) (BackendDoctor, error) {
	var doctor BackendDoctor
	err := b.run(ctx, "doctor", map[string]any{}, &doctor)
	return doctor, err
}

func (b Backend) Info(ctx context.Context, m Manifest) (TrajectoryInfo, error) {
	payload, err := b.datasetPayload(m)
	if err != nil {
		return TrajectoryInfo{}, err
	}
	var info TrajectoryInfo
	if err := b.run(ctx, "info", payload, &info); err != nil {
		return TrajectoryInfo{}, err
	}
	return info, nil
}

func (b Backend) Frame(ctx context.Context, m Manifest, frameIndex int, atomSubset string) (Frame, error) {
	if frameIndex < 0 {
		return Frame{}, errors.New("frame index cannot be negative")
	}
	payload, err := b.datasetPayload(m)
	if err != nil {
		return Frame{}, err
	}
	payload["frame"] = frameIndex
	if atomSubset != "" {
		payload["atom_subset"] = atomSubset
	}
	var frame Frame
	if err := b.run(ctx, "frame", payload, &frame); err != nil {
		return Frame{}, err
	}
	return frame, nil
}

func (b Backend) Analyze(ctx context.Context, m Manifest, request AnalysisRequest) (Trace, error) {
	if strings.TrimSpace(request.Type) == "" {
		return Trace{}, errors.New("analysis type is required")
	}
	payload, err := b.datasetPayload(m)
	if err != nil {
		return Trace{}, err
	}
	payload["id"] = firstNonEmpty(request.ID, request.Type)
	payload["type"] = NormalizeFormat(request.Type)
	payload["selection"] = request.Selection
	payload["selections"] = request.Selections
	payload["reference_frame"] = request.ReferenceFrame
	payload["cutoff"] = request.Cutoff
	var trace Trace
	if err := b.run(ctx, "analyze", payload, &trace); err != nil {
		return Trace{}, err
	}
	return trace, nil
}

func (b Backend) datasetPayload(m Manifest) (map[string]any, error) {
	if len(m.Inputs.Trajectories) == 0 {
		return nil, errors.New("dataset has no trajectories")
	}
	topology, err := b.Store.SafeResolvePath(m.Inputs.Topology.Path)
	if err != nil {
		return nil, err
	}
	trajectory, err := b.Store.SafeResolvePath(m.Inputs.Trajectories[0].Path)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"topology":   topology,
		"trajectory": trajectory,
		"stride":     maxInt(m.Processing.Stride, 1),
	}
	if m.Processing.AtomSubset != "" {
		payload["atom_subset"] = m.Processing.AtomSubset
	}
	return payload, nil
}

func (b Backend) run(ctx context.Context, command string, payload any, out any) error {
	script, cleanup, err := materializeBackendScript()
	if err != nil {
		return err
	}
	defer cleanup()
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, b.Python, script, command)
	if b.Preferred != "" {
		cmd.Env = append(os.Environ(), "MDSRV_BACKEND="+b.Preferred)
	}
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("%s backend %s: %s", b.Python, command, message)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		return fmt.Errorf("decode backend response: %w: %s", err, stdout.String())
	}
	return nil
}

func materializeBackendScript() (string, func(), error) {
	tmp, err := os.CreateTemp("", "mdsrv-backend-*.py")
	if err != nil {
		return "", func() {}, err
	}
	if _, err := tmp.Write(backendScript); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", func() {}, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", func() {}, err
	}
	return tmp.Name(), func() { _ = os.Remove(tmp.Name()) }, nil
}

func EncodeFrameBinary(frame Frame) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("MDSF")
	if err := binary.Write(&buf, binary.LittleEndian, uint16(1)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint32(frame.Frame)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, frame.Time); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(frame.Coordinates))); err != nil {
		return nil, err
	}
	unitCell := normalizedUnitCell(frame.UnitCell)
	for _, row := range unitCell {
		for _, value := range row {
			if err := binary.Write(&buf, binary.LittleEndian, value); err != nil {
				return nil, err
			}
		}
	}
	for _, coord := range frame.Coordinates {
		for _, value := range coord {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return nil, errors.New("frame contains non-finite coordinate")
			}
			if err := binary.Write(&buf, binary.LittleEndian, value); err != nil {
				return nil, err
			}
		}
	}
	return buf.Bytes(), nil
}

func WriteTrace(path, format string, trace Trace) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("trace output path is required")
	}
	if format == "" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	switch NormalizeFormat(format) {
	case "json":
		data, err := json.MarshalIndent(trace, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		return os.WriteFile(path, data, 0o644)
	case "csv", "":
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		defer file.Close()
		writer := csv.NewWriter(file)
		if err := writer.Write([]string{"frame", "time", "value", "unit"}); err != nil {
			return err
		}
		for _, value := range trace.Values {
			if err := writer.Write([]string{
				fmt.Sprintf("%d", value.Frame),
				fmt.Sprintf("%.12g", value.Time),
				fmt.Sprintf("%.12g", value.Value),
				trace.Unit,
			}); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	default:
		return fmt.Errorf("unsupported trace format %q", format)
	}
}

func (s Store) RecordAnalysis(datasetID string, analysis Analysis) error {
	m, err := s.LoadDataset(datasetID)
	if err != nil {
		return err
	}
	var replaced bool
	for i := range m.Analyses {
		if m.Analyses[i].ID == analysis.ID {
			m.Analyses[i] = analysis
			replaced = true
			break
		}
	}
	if !replaced {
		m.Analyses = append(m.Analyses, analysis)
	}
	return WriteManifestFile(s.ManifestPath(datasetID), m)
}

func normalizedUnitCell(values [][3]float32) [3][3]float32 {
	var result [3][3]float32
	for i := 0; i < len(values) && i < 3; i++ {
		result[i] = values[i]
	}
	return result
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
