package mdsrv

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const ManifestVersion = "mdsrv.job/v1"

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Manifest struct {
	Version       string        `json:"version" yaml:"version"`
	Runtime       Runtime       `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Metadata      Metadata      `json:"metadata" yaml:"metadata"`
	Inputs        Inputs        `json:"inputs" yaml:"inputs"`
	Processing    Processing    `json:"processing,omitempty" yaml:"processing,omitempty"`
	Selections    []Selection   `json:"selections,omitempty" yaml:"selections,omitempty"`
	Streaming     Streaming     `json:"streaming,omitempty" yaml:"streaming,omitempty"`
	Analyses      []Analysis    `json:"analyses,omitempty" yaml:"analyses,omitempty"`
	Visualization Visualization `json:"visualization,omitempty" yaml:"visualization,omitempty"`
	Outputs       []Output      `json:"outputs,omitempty" yaml:"outputs,omitempty"`
}

type Metadata struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Source      string `json:"source,omitempty" yaml:"source,omitempty"`
	License     string `json:"license,omitempty" yaml:"license,omitempty"`
	CreatedBy   string `json:"created_by,omitempty" yaml:"created_by,omitempty"`
	CreatedAt   string `json:"created_at,omitempty" yaml:"created_at,omitempty"`
}

type Runtime struct {
	ResourceLimits `json:",inline" yaml:",inline"`
	TimeoutSeconds int `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
}

type Inputs struct {
	Topology     FileRef   `json:"topology" yaml:"topology"`
	Trajectories []FileRef `json:"trajectories" yaml:"trajectories"`
}

type FileRef struct {
	Path         string  `json:"path,omitempty" yaml:"path,omitempty"`
	URL          string  `json:"url,omitempty" yaml:"url,omitempty"`
	Format       string  `json:"format,omitempty" yaml:"format,omitempty"`
	SHA256       string  `json:"sha256,omitempty" yaml:"sha256,omitempty"`
	Bytes        int64   `json:"bytes,omitempty" yaml:"bytes,omitempty"`
	AtomCount    int     `json:"atom_count,omitempty" yaml:"atom_count,omitempty"`
	FrameCount   int     `json:"frame_count,omitempty" yaml:"frame_count,omitempty"`
	TimeStart    float64 `json:"time_start,omitempty" yaml:"time_start,omitempty"`
	TimeEnd      float64 `json:"time_end,omitempty" yaml:"time_end,omitempty"`
	TimeStep     float64 `json:"time_step,omitempty" yaml:"time_step,omitempty"`
	OriginalPath string  `json:"original_path,omitempty" yaml:"original_path,omitempty"`
	TimeUnit     string  `json:"time_unit,omitempty" yaml:"time_unit,omitempty"`
	CoordUnit    string  `json:"coordinate_unit,omitempty" yaml:"coordinate_unit,omitempty"`
}

type SessionRef struct {
	ID          string `json:"id" yaml:"id"`
	Path        string `json:"path" yaml:"path"`
	Version     string `json:"version,omitempty" yaml:"version,omitempty"`
	IsSticky    bool   `json:"is_sticky,omitempty" yaml:"is_sticky,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type Processing struct {
	Stride     int    `json:"stride,omitempty" yaml:"stride,omitempty"`
	AtomSubset string `json:"atom_subset,omitempty" yaml:"atom_subset,omitempty"`
	PBC        PBC    `json:"pbc,omitempty" yaml:"pbc,omitempty"`
}

type PBC struct {
	Unwrap    bool      `json:"unwrap,omitempty" yaml:"unwrap,omitempty"`
	Center    string    `json:"center,omitempty" yaml:"center,omitempty"`
	Superpose Superpose `json:"superpose,omitempty" yaml:"superpose,omitempty"`
}

type Superpose struct {
	Selection      string `json:"selection,omitempty" yaml:"selection,omitempty"`
	ReferenceFrame int    `json:"reference_frame,omitempty" yaml:"reference_frame,omitempty"`
}

type Streaming struct {
	Encoding               string `json:"encoding,omitempty" yaml:"encoding,omitempty"`
	Cache                  string `json:"cache,omitempty" yaml:"cache,omitempty"`
	ChunkSizeFrames        int    `json:"chunk_size_frames,omitempty" yaml:"chunk_size_frames,omitempty"`
	MaterializeChunks      bool   `json:"materialize_chunks,omitempty" yaml:"materialize_chunks,omitempty"`
	AllowAtomSubsetQueries bool   `json:"allow_atom_subset_queries,omitempty" yaml:"allow_atom_subset_queries,omitempty"`
	FrameIndex             string `json:"frame_index,omitempty" yaml:"frame_index,omitempty"`
}

type Selection struct {
	ID          string `json:"id" yaml:"id"`
	Expression  string `json:"expression" yaml:"expression"`
	Kind        string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	AtomCount   int    `json:"atom_count,omitempty" yaml:"atom_count,omitempty"`
}

type Analysis struct {
	ID             string            `json:"id" yaml:"id"`
	Type           string            `json:"type" yaml:"type"`
	Selections     map[string]string `json:"selections,omitempty" yaml:"selections,omitempty"`
	Selection      string            `json:"selection,omitempty" yaml:"selection,omitempty"`
	ReferenceFrame int               `json:"reference_frame,omitempty" yaml:"reference_frame,omitempty"`
	Cutoff         float64           `json:"cutoff,omitempty" yaml:"cutoff,omitempty"`
	Frames         string            `json:"frames,omitempty" yaml:"frames,omitempty"`
	Format         string            `json:"format,omitempty" yaml:"format,omitempty"`
	Output         string            `json:"output,omitempty" yaml:"output,omitempty"`
	// Backend records which engine produced the trace. The engines do not always
	// compute the same quantity under the same analysis name -- measured on a demo
	// trajectory, mdtraj and GROMACS rgyr agree to ~9 significant figures, but
	// their rmsd differs by a consistent factor of ~2.45 (different mass-weighting
	// and fitting conventions). Both write to the same traces/<id>-<type>.csv, so
	// without this field a stored trace cannot be attributed and two runs are
	// silently incomparable.
	Backend string `json:"backend,omitempty" yaml:"backend,omitempty"`
	// Unit records the trace's unit of measure, because the engines do not agree
	// on one: MDTraj reports nm and MDAnalysis reports angstrom for the same
	// analysis. Only the CSV carried the unit, so a consumer reading the manifest
	// saw two analyses of one type with no indication they were 10x apart.
	Unit string `json:"unit,omitempty" yaml:"unit,omitempty"`
}

type Visualization struct {
	Molstar  MolstarVisualization `json:"molstar,omitempty" yaml:"molstar,omitempty"`
	MVS      MVSVisualization     `json:"mvs,omitempty" yaml:"mvs,omitempty"`
	Camera   Camera               `json:"camera,omitempty" yaml:"camera,omitempty"`
	Sessions []SessionRef         `json:"sessions,omitempty" yaml:"sessions,omitempty"`
}

type MolstarVisualization struct {
	State string `json:"state,omitempty" yaml:"state,omitempty"`
}

type MVSVisualization struct {
	Scene string `json:"scene,omitempty" yaml:"scene,omitempty"`
}

type Camera struct {
	Focus string `json:"focus,omitempty" yaml:"focus,omitempty"`
}

type Output struct {
	Type string `json:"type" yaml:"type"`
	Path string `json:"path" yaml:"path"`
}

func LoadManifestFile(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	m, err := DecodeManifest(data, path)
	if err != nil {
		return Manifest{}, err
	}
	return m, m.Validate()
}

func DecodeManifest(data []byte, name string) (Manifest, error) {
	var m Manifest
	if isJSONPath(name) || json.Valid(data) {
		if err := json.Unmarshal(data, &m); err != nil {
			return Manifest{}, fmt.Errorf("decode json: %w", err)
		}
		return m, nil
	}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("decode yaml: %w", err)
	}
	return m, nil
}

func WriteManifestFile(path string, m Manifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (m Manifest) Validate() error {
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("version is required")
	}
	if m.Version != ManifestVersion {
		return fmt.Errorf("unsupported manifest version %q", m.Version)
	}
	if err := ValidateID(m.Metadata.ID); err != nil {
		return fmt.Errorf("metadata.id: %w", err)
	}
	if err := m.Inputs.Topology.Validate("inputs.topology"); err != nil {
		return err
	}
	if len(m.Inputs.Trajectories) == 0 {
		return errors.New("inputs.trajectories must include at least one trajectory")
	}
	for i, trajectory := range m.Inputs.Trajectories {
		if err := trajectory.Validate(fmt.Sprintf("inputs.trajectories[%d]", i)); err != nil {
			return err
		}
	}
	if m.Processing.Stride < 0 {
		return errors.New("processing.stride cannot be negative")
	}
	if m.Streaming.ChunkSizeFrames < 0 {
		return errors.New("streaming.chunk_size_frames cannot be negative")
	}
	if m.Runtime.TimeoutSeconds < 0 {
		return errors.New("runtime.timeout_seconds cannot be negative")
	}
	if err := m.Runtime.ResourceLimits.Validate(); err != nil {
		return fmt.Errorf("runtime: %w", err)
	}
	for i, analysis := range m.Analyses {
		if strings.TrimSpace(analysis.ID) == "" {
			return fmt.Errorf("analyses[%d].id is required", i)
		}
		if strings.TrimSpace(analysis.Type) == "" {
			return fmt.Errorf("analyses[%d].type is required", i)
		}
	}
	for i, output := range m.Outputs {
		if strings.TrimSpace(output.Type) == "" {
			return fmt.Errorf("outputs[%d].type is required", i)
		}
		if strings.TrimSpace(output.Path) == "" {
			return fmt.Errorf("outputs[%d].path is required", i)
		}
	}
	return nil
}

func (ref FileRef) Validate(label string) error {
	var sources int
	if strings.TrimSpace(ref.Path) != "" {
		sources++
	}
	if strings.TrimSpace(ref.URL) != "" {
		sources++
	}
	if sources == 0 {
		return fmt.Errorf("%s: at least one of path or url is required", label)
	}
	if ref.URL != "" {
		parsed, err := url.Parse(ref.URL)
		if err != nil || parsed.Scheme == "" {
			return fmt.Errorf("%s.url must be absolute: %q", label, ref.URL)
		}
	}
	if strings.TrimSpace(ref.Format) == "" {
		return fmt.Errorf("%s.format is required", label)
	}
	return nil
}

// windowsReservedNames cannot be used as a filename on Windows, with or without
// an extension: CON.yaml is as invalid as CON. Ids become filenames
// (datasets/<id>.yaml, topology/<id>.gro), and this project ships Windows
// binaries, so accepting one produces a store that simply cannot be read there.
var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

func ValidateID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("is required")
	}
	if !idPattern.MatchString(id) {
		return errors.New("invalid id: must start with an alphanumeric character and contain only letters, digits, dot, underscore, or dash")
	}
	// Portability, same class as the case-sensitivity check in LoadDataset: a store
	// should be readable wherever it is taken. Windows strips a trailing dot from a
	// filename, so "name." and "name" would collide there.
	if strings.HasSuffix(id, ".") {
		return errors.New("invalid id: must not end with a dot, which Windows strips from filenames")
	}
	stem := id
	if index := strings.Index(stem, "."); index >= 0 {
		stem = stem[:index]
	}
	if windowsReservedNames[strings.ToLower(stem)] {
		return fmt.Errorf("invalid id: %q is a reserved device name on Windows and cannot be a filename there", stem)
	}
	return nil
}

func DefaultDatasetID(path string) string {
	base := filepath.Base(strings.Split(path, "?")[0])
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	var b strings.Builder
	lastDash := false
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '.', r == '_', r == '-':
			if b.Len() > 0 {
				b.WriteRune(r)
				lastDash = false
			}
		default:
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	id := strings.Trim(b.String(), ".-_")
	if id == "" {
		return "dataset"
	}
	return id
}

func InferFormat(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.Split(path, "?")[0])), ".")
	switch ext {
	case "cif", "mmcif":
		return "mmcif"
	case "xtc", "trr", "dcd", "gro", "pdb", "psf", "prmtop", "top", "nc", "netcdf", "nctraj", "lammpstrj", "xyz", "hdf5", "tng":
		return ext
	default:
		return ext
	}
}

func NormalizeFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "cif":
		return "mmcif"
	case "netcdf":
		return "nc"
	default:
		return strings.ToLower(strings.TrimSpace(format))
	}
}

func isJSONPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".json" || ext == ".jsonl"
}
