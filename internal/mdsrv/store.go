package mdsrv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rafflesia-ai/hlmdsrv/internal/gromacs"
)

const (
	DatasetsDir   = "datasets"
	TopologyDir   = "topology"
	TrajectoryDir = "trajectory"
	SessionDir    = "session"
	IndexesDir    = "indexes"
	ChunksDir     = "chunks"
	JobsDir       = "jobs"

	StoreMetadataFile = "store.json"
	StoreVersion      = "mdsrv.store/v1"
)

type Store struct {
	Root string
}

type StoreMetadata struct {
	Version         string `json:"version"`
	ManifestVersion string `json:"manifest_version"`
	CreatedAt       string `json:"created_at"`
}

type StoreDoctorReport struct {
	Store           string           `json:"store"`
	OK              bool             `json:"ok"`
	Version         string           `json:"version,omitempty"`
	ExpectedVersion string           `json:"expected_version"`
	ManifestVersion string           `json:"manifest_version,omitempty"`
	Metadata        string           `json:"metadata"`
	Checks          []StoreCheck     `json:"checks"`
	Migrations      []StoreMigration `json:"migrations,omitempty"`
}

type StoreCheck struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type StoreMigration struct {
	ID       string `json:"id"`
	From     string `json:"from,omitempty"`
	To       string `json:"to"`
	Required bool   `json:"required"`
	Message  string `json:"message"`
}

type SessionOptions struct {
	ID          string `json:"id" yaml:"id"`
	DatasetID   string `json:"dataset_id" yaml:"dataset_id"`
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Source      string `json:"source,omitempty" yaml:"source,omitempty"`
	Version     string `json:"version,omitempty" yaml:"version,omitempty"`
	File        string `json:"file" yaml:"file"`
	IsSticky    bool   `json:"is_sticky,omitempty" yaml:"is_sticky,omitempty"`
	Force       bool   `json:"force,omitempty" yaml:"force,omitempty"`
}

type IngestOptions struct {
	ID            string `json:"id,omitempty" yaml:"id,omitempty"`
	Name          string `json:"name,omitempty" yaml:"name,omitempty"`
	Description   string `json:"description,omitempty" yaml:"description,omitempty"`
	Source        string `json:"source,omitempty" yaml:"source,omitempty"`
	License       string `json:"license,omitempty" yaml:"license,omitempty"`
	CreatedBy     string `json:"created_by,omitempty" yaml:"created_by,omitempty"`
	Topology      string `json:"topology,omitempty" yaml:"topology,omitempty"`
	TopologyURL   string `json:"topology_url,omitempty" yaml:"topology_url,omitempty"`
	Trajectory    string `json:"trajectory,omitempty" yaml:"trajectory,omitempty"`
	TrajectoryURL string `json:"trajectory_url,omitempty" yaml:"trajectory_url,omitempty"`
	Cache         string `json:"cache,omitempty" yaml:"cache,omitempty"`
	Stride        int    `json:"stride,omitempty" yaml:"stride,omitempty"`
	AtomSubset    string `json:"atom_subset,omitempty" yaml:"atom_subset,omitempty"`
	TimeUnit      string `json:"time_unit,omitempty" yaml:"time_unit,omitempty"`
	CoordUnit     string `json:"coordinate_unit,omitempty" yaml:"coordinate_unit,omitempty"`
	Force         bool   `json:"force,omitempty" yaml:"force,omitempty"`

	// AllowedHosts restricts remote URL downloads to these hosts, and is
	// re-checked on every HTTP redirect hop so an allowed host cannot bounce
	// the download to an internal service. It is set by the server from its
	// --allow-host policy and is intentionally not client-settable.
	AllowedHosts []string `json:"-" yaml:"-"`
}

type UpdateOptions struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`
	License     string `json:"license,omitempty"`
}

type GCReport struct {
	Removed []string `json:"removed,omitempty"`
	Kept    []string `json:"kept,omitempty"`
}

type FrameIndex struct {
	DatasetID       string       `json:"dataset_id"`
	FrameCount      int          `json:"frame_count"`
	AtomCount       int          `json:"atom_count,omitempty"`
	TimeStart       float64      `json:"time_start,omitempty"`
	TimeEnd         float64      `json:"time_end,omitempty"`
	TimeStep        float64      `json:"time_step,omitempty"`
	ChunkSizeFrames int          `json:"chunk_size_frames"`
	Frames          []FramePoint `json:"frames,omitempty"`
	Chunks          []FrameChunk `json:"chunks"`
}

type FramePoint struct {
	Index int     `json:"index"`
	Time  float64 `json:"time"`
}

type FrameChunk struct {
	Index    int    `json:"index"`
	Start    int    `json:"start"`
	Stop     int    `json:"stop"`
	Path     string `json:"path,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

type FrameChunkData struct {
	DatasetID string  `json:"dataset_id"`
	Chunk     int     `json:"chunk"`
	Start     int     `json:"start"`
	Stop      int     `json:"stop"`
	Encoding  string  `json:"encoding"`
	Frames    []Frame `json:"frames"`
}

type BuildFrameIndexOptions struct {
	ChunkSize      int
	GromacsCommand string
	Limits         ResourceLimits
}

type BuildFrameChunksOptions struct {
	ChunkSize      int
	Encoding       string
	GromacsCommand string
	Force          bool
	Limits         ResourceLimits
}

type DatasetSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`
	Manifest    string `json:"manifest"`
	Topology    string `json:"topology,omitempty"`
	Trajectory  string `json:"trajectory,omitempty"`
	AtomCount   int    `json:"atom_count,omitempty"`
	FrameCount  int    `json:"frame_count,omitempty"`
}

type SessionSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`
	Version     string `json:"version,omitempty"`
	IsSticky    bool   `json:"is_sticky,omitempty"`
	Path        string `json:"path,omitempty"`
}

type FileCheck struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Bytes  int64  `json:"bytes,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Error  string `json:"error,omitempty"`
}

type ValidationReport struct {
	ID       string      `json:"id"`
	Files    []FileCheck `json:"files"`
	Warnings []string    `json:"warnings,omitempty"`
}

type trajectoryIndexEntry struct {
	Timestamp   int64  `json:"timestamp"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

type sessionIndexEntry struct {
	Timestamp   int64  `json:"timestamp"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Version     string `json:"version"`
	IsSticky    bool   `json:"isSticky,omitempty"`
}

func OpenStore(root string) (Store, error) {
	if strings.TrimSpace(root) == "" {
		return Store{}, errors.New("store path is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Store{}, err
	}
	return Store{Root: absolute}, nil
}

func (s Store) Init() error {
	for _, dir := range []string{DatasetsDir, TopologyDir, TrajectoryDir, SessionDir, IndexesDir, ChunksDir, JobsDir, "visualization"} {
		if err := os.MkdirAll(filepath.Join(s.Root, dir), 0o755); err != nil {
			return err
		}
	}
	if err := ensureJSONArray(filepath.Join(s.Root, "trajectory_index.json")); err != nil {
		return err
	}
	if err := ensureJSONArray(filepath.Join(s.Root, "session_index.json")); err != nil {
		return err
	}
	_, err := s.EnsureMetadata()
	return err
}

func (s Store) MetadataPath() string {
	return filepath.Join(s.Root, StoreMetadataFile)
}

func (s Store) EnsureMetadata() (StoreMetadata, error) {
	metadata, err := s.LoadMetadata()
	if err == nil {
		if metadata.Version != StoreVersion {
			return StoreMetadata{}, fmt.Errorf("unsupported store version %q; expected %q", metadata.Version, StoreVersion)
		}
		return metadata, nil
	}
	if !os.IsNotExist(err) {
		return StoreMetadata{}, err
	}
	metadata = StoreMetadata{
		Version:         StoreVersion,
		ManifestVersion: ManifestVersion,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return StoreMetadata{}, err
	}
	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return StoreMetadata{}, err
	}
	if err := os.WriteFile(s.MetadataPath(), append(raw, '\n'), 0o644); err != nil {
		return StoreMetadata{}, err
	}
	return metadata, nil
}

func (s Store) LoadMetadata() (StoreMetadata, error) {
	raw, err := os.ReadFile(s.MetadataPath())
	if err != nil {
		return StoreMetadata{}, err
	}
	var metadata StoreMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return StoreMetadata{}, err
	}
	if strings.TrimSpace(metadata.Version) == "" {
		return StoreMetadata{}, errors.New("store metadata version is required")
	}
	return metadata, nil
}

func (s Store) Doctor() StoreDoctorReport {
	report := StoreDoctorReport{
		Store:           s.Root,
		OK:              true,
		ExpectedVersion: StoreVersion,
		Metadata:        filepath.ToSlash(s.MetadataPath()),
	}
	addCheck := func(name, path string, ok bool, message string) {
		if !ok {
			report.OK = false
		} else {
			message = ""
		}
		report.Checks = append(report.Checks, StoreCheck{
			Name:    name,
			Path:    filepath.ToSlash(path),
			OK:      ok,
			Message: message,
		})
	}
	if info, err := os.Stat(s.Root); err != nil {
		addCheck("root", s.Root, false, err.Error())
		return report
	} else {
		addCheck("root", s.Root, info.IsDir(), "root is not a directory")
	}
	metadata, err := s.LoadMetadata()
	if err != nil {
		addCheck("metadata", s.MetadataPath(), false, err.Error())
		if os.IsNotExist(err) {
			report.Migrations = append(report.Migrations, StoreMigration{
				ID:       "create-store-metadata",
				To:       StoreVersion,
				Required: false,
				Message:  "run hlmdsrv init --store STORE to write store.json",
			})
		}
	} else {
		report.Version = metadata.Version
		report.ManifestVersion = metadata.ManifestVersion
		addCheck("metadata", s.MetadataPath(), metadata.Version == StoreVersion, fmt.Sprintf("unsupported store version %q", metadata.Version))
		if metadata.Version != StoreVersion {
			report.Migrations = append(report.Migrations, StoreMigration{
				ID:       "unsupported-store-version",
				From:     metadata.Version,
				To:       StoreVersion,
				Required: true,
				Message:  "no automatic migration is available for this store version",
			})
		}
	}
	for _, dir := range []string{DatasetsDir, TopologyDir, TrajectoryDir, SessionDir, IndexesDir, ChunksDir, JobsDir, "visualization"} {
		path := filepath.Join(s.Root, dir)
		if info, err := os.Stat(path); err != nil {
			addCheck(dir, path, false, err.Error())
		} else {
			addCheck(dir, path, info.IsDir(), "path is not a directory")
		}
	}
	for _, file := range []string{"trajectory_index.json", "session_index.json"} {
		path := filepath.Join(s.Root, file)
		raw, err := os.ReadFile(path)
		if err != nil {
			addCheck(file, path, false, err.Error())
			continue
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			addCheck(file, path, false, err.Error())
			continue
		}
		addCheck(file, path, true, "")
	}
	return report
}

func (s Store) Ingest(opts IngestOptions) (Manifest, error) {
	var cleanup []string
	defer func() {
		for _, path := range cleanup {
			_ = os.Remove(path)
		}
	}()
	if opts.TopologyURL != "" {
		path, err := downloadToCache(opts.TopologyURL, firstNonEmpty(opts.Cache, filepath.Join(s.Root, "cache", "downloads")), opts.AllowedHosts)
		if err != nil {
			return Manifest{}, err
		}
		opts.Topology = path
	}
	if opts.TrajectoryURL != "" {
		path, err := downloadToCache(opts.TrajectoryURL, firstNonEmpty(opts.Cache, filepath.Join(s.Root, "cache", "downloads")), opts.AllowedHosts)
		if err != nil {
			return Manifest{}, err
		}
		opts.Trajectory = path
	}
	if strings.TrimSpace(opts.ID) == "" {
		opts.ID = DefaultDatasetID(opts.Trajectory)
	}
	if err := ValidateID(opts.ID); err != nil {
		return Manifest{}, fmt.Errorf("id: %w", err)
	}
	if strings.TrimSpace(opts.Topology) == "" {
		return Manifest{}, errors.New("topology path is required")
	}
	if strings.TrimSpace(opts.Trajectory) == "" {
		return Manifest{}, errors.New("trajectory path is required")
	}
	if opts.Stride < 0 {
		return Manifest{}, errors.New("stride cannot be negative")
	}
	if err := s.Init(); err != nil {
		return Manifest{}, err
	}
	if !opts.Force {
		for _, path := range []string{
			s.ManifestPath(opts.ID),
			fileTarget(s.Root, TopologyDir, opts.ID, opts.Topology),
			fileTarget(s.Root, TrajectoryDir, opts.ID, opts.Trajectory),
		} {
			if _, err := os.Stat(path); err == nil {
				return Manifest{}, fmt.Errorf("%s already exists; pass --force to overwrite", path)
			}
		}
	}

	topologyTarget := fileTarget(s.Root, TopologyDir, opts.ID, opts.Topology)
	topologyHash, topologyBytes, err := copyWithChecksum(opts.Topology, topologyTarget, opts.Force)
	if err != nil {
		return Manifest{}, fmt.Errorf("copy topology: %w", err)
	}
	trajectoryTarget := fileTarget(s.Root, TrajectoryDir, opts.ID, opts.Trajectory)
	trajectoryHash, trajectoryBytes, err := copyWithChecksum(opts.Trajectory, trajectoryTarget, opts.Force)
	if err != nil {
		return Manifest{}, fmt.Errorf("copy trajectory: %w", err)
	}

	topologyFormat := NormalizeFormat(InferFormat(opts.Topology))
	trajectoryFormat := NormalizeFormat(InferFormat(opts.Trajectory))
	if opts.TimeUnit == "" {
		opts.TimeUnit = "ps"
	}
	if opts.CoordUnit == "" {
		opts.CoordUnit = "nm"
	}
	now := time.Now().UTC()
	m := Manifest{
		Version: ManifestVersion,
		Metadata: Metadata{
			ID:          opts.ID,
			Name:        firstNonEmpty(opts.Name, opts.ID),
			Description: opts.Description,
			Source:      opts.Source,
			License:     opts.License,
			CreatedBy:   opts.CreatedBy,
			CreatedAt:   now.Format(time.RFC3339),
		},
		Inputs: Inputs{
			Topology: FileRef{
				Path:         storeRelPath(s.Root, topologyTarget),
				URL:          opts.TopologyURL,
				Format:       topologyFormat,
				SHA256:       topologyHash,
				Bytes:        topologyBytes,
				OriginalPath: opts.Topology,
			},
			Trajectories: []FileRef{{
				Path:         storeRelPath(s.Root, trajectoryTarget),
				URL:          opts.TrajectoryURL,
				Format:       trajectoryFormat,
				SHA256:       trajectoryHash,
				Bytes:        trajectoryBytes,
				OriginalPath: opts.Trajectory,
				TimeUnit:     opts.TimeUnit,
				CoordUnit:    opts.CoordUnit,
			}},
		},
		Processing: Processing{
			Stride:     opts.Stride,
			AtomSubset: opts.AtomSubset,
		},
		Streaming: Streaming{
			Encoding:               "mdsrv-frame-v1",
			Cache:                  filepath.ToSlash(filepath.Join("cache", opts.ID)),
			ChunkSizeFrames:        128,
			AllowAtomSubsetQueries: true,
		},
		Outputs: []Output{
			{Type: "manifest", Path: storeRelPath(s.Root, s.ManifestPath(opts.ID))},
			{Type: "server-store", Path: "."},
		},
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	if err := WriteManifestFile(s.ManifestPath(opts.ID), m); err != nil {
		return Manifest{}, err
	}
	if err := s.UpsertTrajectoryIndex(m, now.Unix()); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (s Store) UpdateDataset(id string, opts UpdateOptions) (Manifest, error) {
	m, err := s.LoadDataset(id)
	if err != nil {
		return Manifest{}, err
	}
	if opts.Name != "" {
		m.Metadata.Name = opts.Name
	}
	if opts.Description != "" {
		m.Metadata.Description = opts.Description
	}
	if opts.Source != "" {
		m.Metadata.Source = opts.Source
	}
	if opts.License != "" {
		m.Metadata.License = opts.License
	}
	if err := WriteManifestFile(s.ManifestPath(id), m); err != nil {
		return Manifest{}, err
	}
	if err := s.UpsertTrajectoryIndex(m, time.Now().UTC().Unix()); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (s Store) RenameDataset(oldID, newID string) (Manifest, error) {
	if err := ValidateID(newID); err != nil {
		return Manifest{}, fmt.Errorf("new id: %w", err)
	}
	m, err := s.LoadDataset(oldID)
	if err != nil {
		return Manifest{}, err
	}
	if _, err := os.Stat(s.ManifestPath(newID)); err == nil {
		return Manifest{}, fmt.Errorf("dataset %s already exists", newID)
	}
	oldManifest := s.ManifestPath(oldID)
	newManifest := s.ManifestPath(newID)
	m.Metadata.ID = newID
	m.Outputs = []Output{{Type: "manifest", Path: storeRelPath(s.Root, newManifest)}, {Type: "server-store", Path: "."}}
	if err := WriteManifestFile(newManifest, m); err != nil {
		return Manifest{}, err
	}
	if err := os.Remove(oldManifest); err != nil {
		return Manifest{}, err
	}
	if err := s.RemoveTrajectoryIndex(oldID); err != nil {
		return Manifest{}, err
	}
	if err := s.UpsertTrajectoryIndex(m, time.Now().UTC().Unix()); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (s Store) DeleteDataset(id string, deleteFiles bool) error {
	m, err := s.LoadDataset(id)
	if err != nil {
		return err
	}
	if deleteFiles {
		paths := []string{m.Inputs.Topology.Path, filepath.ToSlash(filepath.Join(IndexesDir, id+"-frame-index.json"))}
		for _, trajectory := range m.Inputs.Trajectories {
			paths = append(paths, trajectory.Path)
		}
		for _, analysis := range m.Analyses {
			if analysis.Output != "" {
				paths = append(paths, analysis.Output)
			}
		}
		if index, err := s.LoadFrameIndex(id); err == nil {
			for _, chunk := range index.Chunks {
				if chunk.Path != "" {
					paths = append(paths, chunk.Path)
				}
			}
		}
		for _, path := range paths {
			if resolved, err := s.SafeResolvePath(path); err == nil {
				_ = os.Remove(resolved)
			}
		}
	}
	if err := os.Remove(s.ManifestPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.RemoveTrajectoryIndex(id)
}

func (s Store) PublishSession(opts SessionOptions) (SessionRef, error) {
	if err := ValidateID(opts.ID); err != nil {
		return SessionRef{}, fmt.Errorf("id: %w", err)
	}
	if err := ValidateID(opts.DatasetID); err != nil {
		return SessionRef{}, fmt.Errorf("dataset id: %w", err)
	}
	if strings.TrimSpace(opts.File) == "" {
		return SessionRef{}, errors.New("session file path is required")
	}
	if opts.Version == "" {
		opts.Version = "3.4.0"
	}
	if err := s.Init(); err != nil {
		return SessionRef{}, err
	}
	m, err := s.LoadDataset(opts.DatasetID)
	if err != nil {
		return SessionRef{}, err
	}
	target := fileTarget(s.Root, SessionDir, opts.ID, opts.File)
	if _, _, err := copyWithChecksum(opts.File, target, opts.Force); err != nil {
		return SessionRef{}, fmt.Errorf("copy session: %w", err)
	}
	ref := SessionRef{
		ID:          opts.ID,
		Path:        storeRelPath(s.Root, target),
		Version:     opts.Version,
		IsSticky:    opts.IsSticky,
		Description: opts.Description,
	}
	m.Visualization.Sessions = upsertSessionRef(m.Visualization.Sessions, ref)
	if m.Visualization.Molstar.State == "" && strings.EqualFold(filepath.Ext(ref.Path), ".molj") {
		m.Visualization.Molstar.State = ref.Path
	}
	if err := WriteManifestFile(s.ManifestPath(opts.DatasetID), m); err != nil {
		return SessionRef{}, err
	}
	if err := s.UpsertSessionIndex(sessionIndexEntry{
		Timestamp:   time.Now().UTC().Unix(),
		ID:          opts.ID,
		Name:        firstNonEmpty(opts.Name, m.Metadata.Name, opts.ID),
		Description: firstNonEmpty(opts.Description, m.Metadata.Description),
		Source:      firstNonEmpty(opts.Source, m.Metadata.Source),
		Version:     opts.Version,
		IsSticky:    opts.IsSticky,
	}); err != nil {
		return SessionRef{}, err
	}
	return ref, nil
}

func (s Store) ManifestPath(id string) string {
	return filepath.Join(s.Root, DatasetsDir, id+".yaml")
}

func (s Store) LoadDataset(id string) (Manifest, error) {
	if err := ValidateID(id); err != nil {
		return Manifest{}, fmt.Errorf("id: %w", err)
	}
	return LoadManifestFile(s.ManifestPath(id))
}

func (s Store) ProbeDataset(ctx context.Context, id string, gromacsCommand string) (Manifest, error) {
	m, err := s.LoadDataset(id)
	if err != nil {
		return Manifest{}, err
	}
	return s.ProbeManifest(ctx, m, gromacsCommand)
}

func (s Store) ProbeManifest(ctx context.Context, m Manifest, gromacsCommand string) (Manifest, error) {
	if len(m.Inputs.Trajectories) == 0 {
		return Manifest{}, errors.New("dataset has no trajectory")
	}
	trajectoryPath, err := s.SafeResolvePath(m.Inputs.Trajectories[0].Path)
	if err != nil {
		return Manifest{}, err
	}
	gmx := gromacs.New(gromacs.Options{Command: gromacsCommand})
	if err := gmx.RequireAvailable(); err != nil {
		return Manifest{}, err
	}
	probe, err := gmx.Probe(ctx, trajectoryPath)
	if err != nil {
		return Manifest{}, err
	}
	ApplyProbe(&m.Inputs.Trajectories[0], probe)
	if err := WriteManifestFile(s.ManifestPath(m.Metadata.ID), m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (s Store) ExtractFrame(ctx context.Context, id string, frameIndex int, outputPath string, gromacsCommand string) (Manifest, error) {
	m, err := s.LoadDataset(id)
	if err != nil {
		return Manifest{}, err
	}
	if len(m.Inputs.Trajectories) == 0 {
		return Manifest{}, errors.New("dataset has no trajectory")
	}
	probe := ProbeFromFileRef(m.Inputs.Trajectories[0])
	if probe.FrameCount == 0 {
		m, err = s.ProbeManifest(ctx, m, gromacsCommand)
		if err != nil {
			return Manifest{}, err
		}
		probe = ProbeFromFileRef(m.Inputs.Trajectories[0])
	}
	topologyPath, err := s.SafeResolvePath(m.Inputs.Topology.Path)
	if err != nil {
		return Manifest{}, err
	}
	trajectoryPath, err := s.SafeResolvePath(m.Inputs.Trajectories[0].Path)
	if err != nil {
		return Manifest{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return Manifest{}, err
	}
	gmx := gromacs.New(gromacs.Options{Command: gromacsCommand})
	if err := gmx.RequireAvailable(); err != nil {
		return Manifest{}, err
	}
	if timeValue, ok, err := s.frameTimeFromIndex(m, frameIndex); err != nil {
		return Manifest{}, err
	} else if ok {
		if err := gmx.ExtractFrame(ctx, gromacs.ExtractFrameOptions{
			Topology:   topologyPath,
			Trajectory: trajectoryPath,
			Output:     outputPath,
			Time:       &timeValue,
		}); err != nil {
			return Manifest{}, err
		}
		return m, nil
	}
	if err := gmx.ExtractFrame(ctx, gromacs.ExtractFrameOptions{
		Topology:   topologyPath,
		Trajectory: trajectoryPath,
		Output:     outputPath,
		FrameIndex: frameIndex,
		Probe:      probe,
	}); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (s Store) frameTimeFromIndex(m Manifest, frameIndex int) (float64, bool, error) {
	if frameIndex < 0 {
		return 0, false, errors.New("frame index cannot be negative")
	}
	if m.Streaming.FrameIndex == "" {
		return 0, false, nil
	}
	resolved, err := s.SafeResolvePath(m.Streaming.FrameIndex)
	if err != nil {
		return 0, false, err
	}
	var index FrameIndex
	if err := readJSONFile(resolved, &index); err != nil {
		return 0, false, err
	}
	if index.FrameCount > 0 && frameIndex >= index.FrameCount {
		return 0, false, fmt.Errorf("frame index %d is out of range for %d frames", frameIndex, index.FrameCount)
	}
	for _, point := range index.Frames {
		if point.Index == frameIndex {
			return point.Time, true, nil
		}
	}
	return 0, false, nil
}

// isHiddenSidecar reports whether a path is a dotfile rather than a dataset
// manifest the store wrote. macOS creates AppleDouble "._<name>" siblings for
// extended attributes on every non-native filesystem (exFAT, FAT, SMB, USB) —
// exactly where multi-GB trajectory stores tend to live — and those match a
// "*.yaml" glob while being binary. Treating one as a manifest made a valid store
// unlistable and unpublishable, blaming a file the user never created. Any
// leading-dot file is skipped, not just AppleDouble: the store never writes one,
// so a dotfile is never ours to parse.
func isHiddenSidecar(path string) bool {
	return strings.HasPrefix(filepath.Base(path), ".")
}

func (s Store) ListDatasets() ([]DatasetSummary, error) {
	matches, err := filepath.Glob(filepath.Join(s.Root, DatasetsDir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	var summaries []DatasetSummary
	for _, path := range matches {
		if isHiddenSidecar(path) {
			continue
		}
		m, err := LoadManifestFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		summary := DatasetSummary{
			ID:          m.Metadata.ID,
			Name:        m.Metadata.Name,
			Description: m.Metadata.Description,
			Source:      m.Metadata.Source,
			Manifest:    storeRelPath(s.Root, path),
			Topology:    m.Inputs.Topology.Path,
		}
		if len(m.Inputs.Trajectories) > 0 {
			summary.Trajectory = m.Inputs.Trajectories[0].Path
			summary.AtomCount = m.Inputs.Trajectories[0].AtomCount
			summary.FrameCount = m.Inputs.Trajectories[0].FrameCount
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func (s Store) ListSessions() ([]SessionSummary, error) {
	var entries []sessionIndexEntry
	if err := readJSONFile(filepath.Join(s.Root, "session_index.json"), &entries); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	summaries := make([]SessionSummary, 0, len(entries))
	for _, entry := range entries {
		summaries = append(summaries, SessionSummary{
			ID:          entry.ID,
			Name:        entry.Name,
			Description: entry.Description,
			Source:      entry.Source,
			Version:     entry.Version,
			IsSticky:    entry.IsSticky,
			Path:        sessionPathForID(s.Root, entry.ID),
		})
	}
	return summaries, nil
}

func (s Store) SaveSelection(datasetID string, selection Selection) (Selection, error) {
	if err := ValidateID(selection.ID); err != nil {
		return Selection{}, fmt.Errorf("selection id: %w", err)
	}
	if strings.TrimSpace(selection.Expression) == "" {
		return Selection{}, errors.New("selection expression is required")
	}
	m, err := s.LoadDataset(datasetID)
	if err != nil {
		return Selection{}, err
	}
	if selection.Kind == "" {
		selection.Kind = "atom-index"
	}
	if selection.Kind == "atom-index" && len(m.Inputs.Trajectories) > 0 && m.Inputs.Trajectories[0].AtomCount > 0 {
		atoms, err := ParseAtomSelection(selection.Expression, m.Inputs.Trajectories[0].AtomCount)
		if err != nil {
			return Selection{}, err
		}
		selection.AtomCount = len(atoms)
	}
	var replaced bool
	for i := range m.Selections {
		if m.Selections[i].ID == selection.ID {
			m.Selections[i] = selection
			replaced = true
			break
		}
	}
	if !replaced {
		m.Selections = append(m.Selections, selection)
	}
	return selection, WriteManifestFile(s.ManifestPath(datasetID), m)
}

func (s Store) DeleteSelection(datasetID, selectionID string) error {
	m, err := s.LoadDataset(datasetID)
	if err != nil {
		return err
	}
	var next []Selection
	for _, selection := range m.Selections {
		if selection.ID != selectionID {
			next = append(next, selection)
		}
	}
	m.Selections = next
	return WriteManifestFile(s.ManifestPath(datasetID), m)
}

func (s Store) BuildFrameIndex(ctx context.Context, id string, chunkSize int, gromacsCommand string) (FrameIndex, error) {
	return s.BuildFrameIndexWithOptions(ctx, id, BuildFrameIndexOptions{
		ChunkSize:      chunkSize,
		GromacsCommand: gromacsCommand,
	})
}

func (s Store) BuildFrameIndexWithOptions(ctx context.Context, id string, opts BuildFrameIndexOptions) (FrameIndex, error) {
	if err := opts.Limits.Validate(); err != nil {
		return FrameIndex{}, err
	}
	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 128
	}
	m, err := s.LoadDataset(id)
	if err != nil {
		return FrameIndex{}, err
	}
	if len(m.Inputs.Trajectories) == 0 {
		return FrameIndex{}, errors.New("dataset has no trajectory")
	}
	report := ProbeFromFileRef(m.Inputs.Trajectories[0])
	if trajectoryPath, pathErr := s.SafeResolvePath(m.Inputs.Trajectories[0].Path); pathErr == nil {
		gmx := gromacs.New(gromacs.Options{Command: opts.GromacsCommand})
		if gmx.Available() {
			if probed, probeErr := gmx.Probe(ctx, trajectoryPath); probeErr == nil {
				report = probed
				ApplyProbe(&m.Inputs.Trajectories[0], probed)
			} else if report.FrameCount == 0 {
				return FrameIndex{}, probeErr
			}
		}
	}
	if report.FrameCount == 0 {
		m, err = s.ProbeManifest(ctx, m, opts.GromacsCommand)
		if err != nil {
			return FrameIndex{}, err
		}
		report = ProbeFromFileRef(m.Inputs.Trajectories[0])
	}
	if err := opts.Limits.CheckProbe("trajectory", report); err != nil {
		return FrameIndex{}, err
	}
	index := FrameIndex{
		DatasetID:       id,
		FrameCount:      report.FrameCount,
		AtomCount:       report.AtomCount,
		TimeStart:       report.TimeStart,
		TimeEnd:         report.TimeEnd,
		TimeStep:        report.TimeStep,
		ChunkSizeFrames: chunkSize,
	}
	index.Frames = framePoints(report)
	for start, chunk := 0, 0; start < report.FrameCount; start, chunk = start+chunkSize, chunk+1 {
		stop := start + chunkSize
		if stop > report.FrameCount {
			stop = report.FrameCount
		}
		index.Chunks = append(index.Chunks, FrameChunk{Index: chunk, Start: start, Stop: stop})
	}
	path := filepath.Join(s.Root, IndexesDir, id+"-frame-index.json")
	if err := writeJSONFile(path, index); err != nil {
		return FrameIndex{}, err
	}
	m.Streaming.FrameIndex = filepath.ToSlash(filepath.Join(IndexesDir, id+"-frame-index.json"))
	m.Streaming.ChunkSizeFrames = chunkSize
	if err := WriteManifestFile(s.ManifestPath(id), m); err != nil {
		return FrameIndex{}, err
	}
	return index, nil
}

func framePoints(probe gromacs.TrajectoryProbe) []FramePoint {
	if probe.FrameCount <= 0 {
		return nil
	}
	points := make([]FramePoint, 0, probe.FrameCount)
	if len(probe.FrameTimes) == probe.FrameCount {
		for i, timeValue := range probe.FrameTimes {
			points = append(points, FramePoint{Index: i, Time: timeValue})
		}
		return points
	}
	if probe.FrameCount == 1 || probe.TimeStep > 0 || probe.TimeStart != 0 {
		for i := 0; i < probe.FrameCount; i++ {
			points = append(points, FramePoint{Index: i, Time: probe.TimeStart + float64(i)*probe.TimeStep})
		}
	}
	return points
}

func (s Store) LoadFrameIndex(id string) (FrameIndex, error) {
	m, err := s.LoadDataset(id)
	if err != nil {
		return FrameIndex{}, err
	}
	indexPath := m.Streaming.FrameIndex
	if indexPath == "" {
		indexPath = filepath.ToSlash(filepath.Join(IndexesDir, id+"-frame-index.json"))
	}
	resolved, err := s.SafeResolvePath(indexPath)
	if err != nil {
		return FrameIndex{}, err
	}
	var index FrameIndex
	if err := readJSONFile(resolved, &index); err != nil {
		return FrameIndex{}, err
	}
	if index.DatasetID == "" {
		return FrameIndex{}, fmt.Errorf("frame index %s is empty or invalid", indexPath)
	}
	return index, nil
}

func (s Store) BuildFrameChunks(ctx context.Context, id string, chunkSize int, gromacsCommand string, force bool) (FrameIndex, error) {
	return s.BuildFrameChunksWithOptions(ctx, id, BuildFrameChunksOptions{
		ChunkSize:      chunkSize,
		GromacsCommand: gromacsCommand,
		Force:          force,
	})
}

func (s Store) BuildFrameChunksWithOptions(ctx context.Context, id string, opts BuildFrameChunksOptions) (FrameIndex, error) {
	encoding, err := NormalizeFrameChunkEncoding(opts.Encoding)
	if err != nil {
		return FrameIndex{}, err
	}
	index, err := s.BuildFrameIndexWithOptions(ctx, id, BuildFrameIndexOptions{
		ChunkSize:      opts.ChunkSize,
		GromacsCommand: opts.GromacsCommand,
		Limits:         opts.Limits,
	})
	if err != nil {
		return FrameIndex{}, err
	}
	m, err := s.LoadDataset(id)
	if err != nil {
		return FrameIndex{}, err
	}
	if len(m.Inputs.Trajectories) == 0 {
		return FrameIndex{}, errors.New("dataset has no trajectory")
	}
	chunkRoot := filepath.Join(s.Root, ChunksDir, id)
	if err := os.MkdirAll(chunkRoot, 0o755); err != nil {
		return FrameIndex{}, err
	}
	for i := range index.Chunks {
		chunk := &index.Chunks[i]
		relPath := filepath.ToSlash(filepath.Join(ChunksDir, id, fmt.Sprintf("chunk-%06d%s", chunk.Index, FrameChunkExtension(encoding))))
		path := filepath.Join(s.Root, filepath.FromSlash(relPath))
		if _, err := os.Stat(path); err == nil && !opts.Force {
			chunk.Path = relPath
			chunk.Encoding = encoding
			continue
		}
		data := FrameChunkData{
			DatasetID: id,
			Chunk:     chunk.Index,
			Start:     chunk.Start,
			Stop:      chunk.Stop,
			Encoding:  encoding,
			Frames:    make([]Frame, 0, chunk.Stop-chunk.Start),
		}
		for frameIndex := chunk.Start; frameIndex < chunk.Stop; frameIndex++ {
			tmp, err := os.CreateTemp("", fmt.Sprintf("mdsrv-chunk-%s-%d-*.gro", id, frameIndex))
			if err != nil {
				return FrameIndex{}, err
			}
			outputPath := tmp.Name()
			_ = tmp.Close()
			if _, err := s.ExtractFrame(ctx, id, frameIndex, outputPath, opts.GromacsCommand); err != nil {
				_ = os.Remove(outputPath)
				return FrameIndex{}, err
			}
			frame, err := ParseGROFrame(outputPath, frameIndex)
			_ = os.Remove(outputPath)
			if err != nil {
				return FrameIndex{}, err
			}
			if err := opts.Limits.CheckFrame(fmt.Sprintf("frame %d", frameIndex), frame); err != nil {
				return FrameIndex{}, err
			}
			data.Frames = append(data.Frames, frame)
		}
		encoded, storedEncoding, err := EncodeFrameChunk(data)
		if err != nil {
			return FrameIndex{}, err
		}
		if err := opts.Limits.CheckChunkBytes(relPath, len(encoded)); err != nil {
			return FrameIndex{}, err
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			return FrameIndex{}, err
		}
		chunk.Path = relPath
		chunk.Encoding = storedEncoding
	}
	indexPath := filepath.Join(s.Root, IndexesDir, id+"-frame-index.json")
	if err := writeJSONFile(indexPath, index); err != nil {
		return FrameIndex{}, err
	}
	m.Streaming.FrameIndex = filepath.ToSlash(filepath.Join(IndexesDir, id+"-frame-index.json"))
	m.Streaming.ChunkSizeFrames = index.ChunkSizeFrames
	m.Streaming.Cache = filepath.ToSlash(filepath.Join(ChunksDir, id))
	if err := WriteManifestFile(s.ManifestPath(id), m); err != nil {
		return FrameIndex{}, err
	}
	return index, nil
}

func (s Store) LoadFrameChunk(id string, chunkIndex int) (FrameChunkData, error) {
	file, err := s.LoadFrameChunkFile(id, chunkIndex)
	if err != nil {
		return FrameChunkData{}, err
	}
	data, err := DecodeFrameChunk(file.Bytes, file.Encoding)
	if err != nil {
		return FrameChunkData{}, err
	}
	if data.DatasetID == "" {
		return FrameChunkData{}, fmt.Errorf("frame chunk %s is empty or invalid", file.Path)
	}
	return data, nil
}

func (s Store) LoadFrameChunkFile(id string, chunkIndex int) (FrameChunkFile, error) {
	index, err := s.LoadFrameIndex(id)
	if err != nil {
		return FrameChunkFile{}, err
	}
	if chunkIndex < 0 || chunkIndex >= len(index.Chunks) {
		return FrameChunkFile{}, fmt.Errorf("chunk index %d is out of range", chunkIndex)
	}
	chunk := index.Chunks[chunkIndex]
	path := chunk.Path
	if path == "" {
		path = filepath.ToSlash(filepath.Join(ChunksDir, id, fmt.Sprintf("chunk-%06d.json", chunkIndex)))
	}
	resolved, err := s.SafeResolvePath(path)
	if err != nil {
		return FrameChunkFile{}, err
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return FrameChunkFile{}, err
	}
	encoding := chunk.Encoding
	if encoding == "" {
		encoding = inferFrameChunkEncoding(raw, path)
	}
	return FrameChunkFile{
		Path:        path,
		Encoding:    encoding,
		ContentType: FrameChunkContentType(encoding),
		Bytes:       raw,
	}, nil
}

func (s Store) GC() (GCReport, error) {
	datasets, err := s.ListDatasets()
	if err != nil {
		return GCReport{}, err
	}
	referenced := map[string]bool{"trajectory_index.json": true, "session_index.json": true}
	for _, dataset := range datasets {
		m, err := s.LoadDataset(dataset.ID)
		if err != nil {
			return GCReport{}, err
		}
		referenced[filepath.ToSlash(filepath.Join(DatasetsDir, dataset.ID+".yaml"))] = true
		referenced[m.Inputs.Topology.Path] = true
		for _, trajectory := range m.Inputs.Trajectories {
			referenced[trajectory.Path] = true
		}
		for _, analysis := range m.Analyses {
			if analysis.Output != "" {
				referenced[analysis.Output] = true
			}
		}
		if m.Streaming.FrameIndex != "" {
			referenced[m.Streaming.FrameIndex] = true
			if index, err := s.LoadFrameIndex(dataset.ID); err == nil {
				for _, chunk := range index.Chunks {
					if chunk.Path != "" {
						referenced[chunk.Path] = true
					}
				}
			}
		}
		if m.Visualization.MVS.Scene != "" {
			referenced[m.Visualization.MVS.Scene] = true
		}
		if m.Visualization.Molstar.State != "" {
			referenced[m.Visualization.Molstar.State] = true
		}
		for _, session := range m.Visualization.Sessions {
			referenced[session.Path] = true
		}
	}
	var report GCReport
	for _, dir := range []string{DatasetsDir, TopologyDir, TrajectoryDir, SessionDir, IndexesDir, ChunksDir, "traces", "visualization"} {
		root := filepath.Join(s.Root, dir)
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel := storeRelPath(s.Root, path)
			if referenced[rel] {
				report.Kept = append(report.Kept, rel)
				return nil
			}
			_ = os.Remove(path)
			report.Removed = append(report.Removed, rel)
			return nil
		})
	}
	sort.Strings(report.Kept)
	sort.Strings(report.Removed)
	return report, nil
}

func (s Store) CheckDataset(m Manifest) ValidationReport {
	report := ValidationReport{ID: m.Metadata.ID}
	report.Files = append(report.Files, s.checkFile(m.Inputs.Topology))
	for _, trajectory := range m.Inputs.Trajectories {
		check := s.checkFile(trajectory)
		report.Files = append(report.Files, check)
		if NormalizeFormat(trajectory.Format) != "xtc" {
			report.Warnings = append(report.Warnings, fmt.Sprintf("trajectory %q has format %q; the current Mol*-based MDsrv streaming-server docs describe XTC as the streaming baseline", trajectory.Path, trajectory.Format))
		}
	}
	return report
}

func (s Store) ResolvePath(path string) string {
	resolved, err := s.SafeResolvePath(path)
	if err != nil {
		return filepath.Join(s.Root, filepath.FromSlash(path))
	}
	return resolved
}

func (s Store) SafeResolvePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is required")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	resolved := clean
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(s.Root, clean)
	}
	relative, err := filepath.Rel(s.Root, resolved)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s escapes store root %s", path, s.Root)
	}
	return resolved, nil
}

func (s Store) UpsertTrajectoryIndex(m Manifest, timestamp int64) error {
	if len(m.Inputs.Trajectories) == 0 {
		return nil
	}
	path := filepath.Join(s.Root, "trajectory_index.json")
	var entries []trajectoryIndexEntry
	if err := readJSONFile(path, &entries); err != nil {
		return err
	}
	next := trajectoryIndexEntry{
		Timestamp:   timestamp,
		ID:          m.Metadata.ID,
		Name:        firstNonEmpty(m.Metadata.Name, m.Metadata.ID),
		Description: m.Metadata.Description,
		Source:      m.Metadata.Source,
	}
	var replaced bool
	for i := range entries {
		if entries[i].ID == next.ID {
			entries[i] = next
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, next)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return writeJSONFile(path, entries)
}

func (s Store) RemoveTrajectoryIndex(id string) error {
	path := filepath.Join(s.Root, "trajectory_index.json")
	var entries []trajectoryIndexEntry
	if err := readJSONFile(path, &entries); err != nil {
		return err
	}
	var next []trajectoryIndexEntry
	for _, entry := range entries {
		if entry.ID != id {
			next = append(next, entry)
		}
	}
	return writeJSONFile(path, next)
}

func (s Store) UpsertSessionIndex(next sessionIndexEntry) error {
	path := filepath.Join(s.Root, "session_index.json")
	var entries []sessionIndexEntry
	if err := readJSONFile(path, &entries); err != nil {
		return err
	}
	var replaced bool
	for i := range entries {
		if entries[i].ID == next.ID {
			entries[i] = next
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, next)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return writeJSONFile(path, entries)
}

func (s Store) checkFile(ref FileRef) FileCheck {
	check := FileCheck{Path: ref.Path}
	if ref.Path == "" {
		check.Path = ref.URL
		check.Exists = ref.URL != ""
		return check
	}
	path, err := s.SafeResolvePath(ref.Path)
	if err != nil {
		check.Error = err.Error()
		return check
	}
	info, err := os.Stat(path)
	if err != nil {
		check.Error = err.Error()
		return check
	}
	if info.IsDir() {
		check.Error = "is a directory"
		return check
	}
	check.Exists = true
	check.Bytes = info.Size()
	if ref.SHA256 != "" {
		hash, _, err := checksumFile(path)
		if err != nil {
			check.Error = err.Error()
		} else {
			check.SHA256 = hash
			if !strings.EqualFold(hash, ref.SHA256) {
				check.Error = "sha256 mismatch"
			}
		}
	}
	return check
}

func fileTarget(root, dir, id, source string) string {
	ext := filepath.Ext(source)
	if ext == "" {
		ext = "." + firstNonEmpty(InferFormat(source), "dat")
	}
	return filepath.Join(root, dir, id+strings.ToLower(ext))
}

func sessionPathForID(root, id string) string {
	matches, err := filepath.Glob(filepath.Join(root, SessionDir, id+".*"))
	if err == nil && len(matches) > 0 {
		sort.Strings(matches)
		return storeRelPath(root, matches[0])
	}
	return ""
}

// ensureURLHostAllowed permits the download when no allowlist is configured,
// otherwise requires the URL host to match one of the allowed hosts. It is
// applied to the initial URL and to every redirect target.
func ensureURLHostAllowed(rawURL string, allowedHosts []string) error {
	if len(allowedHosts) == 0 {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	host := strings.ToLower(parsed.Hostname())
	for _, allowed := range allowedHosts {
		if allowed = strings.ToLower(strings.TrimSpace(allowed)); allowed != "" && allowed == host {
			return nil
		}
	}
	return fmt.Errorf("url host %q is not allowed", host)
}

func downloadToCache(rawURL, cacheDir string, allowedHosts []string) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	if err := ensureURLHostAllowed(rawURL, allowedHosts); err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after %d redirects", len(via))
			}
			// Re-check the allowlist on every hop: without this an allowed
			// host could redirect the download to an internal service.
			return ensureURLHostAllowed(req.URL.String(), allowedHosts)
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download %s: status %d", rawURL, resp.StatusCode)
	}
	name := filepath.Base(strings.Split(rawURL, "?")[0])
	if name == "." || name == "/" || name == "" {
		name = "download"
	}
	target := filepath.Join(cacheDir, name)
	tmp := target + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", closeErr
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return target, nil
}

func copyWithChecksum(source, target string, force bool) (string, int64, error) {
	if samePath(source, target) {
		return checksumFile(source)
	}
	if _, err := os.Stat(target); err == nil && !force {
		return "", 0, fmt.Errorf("%s already exists", target)
	}
	input, err := os.Open(source)
	if err != nil {
		return "", 0, err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", 0, err
	}
	tmp := target + ".tmp"
	output, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", 0, err
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hasher), input)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", 0, closeErr
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, nil
}

func checksumFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hasher := sha256.New()
	written, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, nil
}

func ensureJSONArray(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeJSONFile(path, []any{})
}

func readJSONFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	return json.Unmarshal(data, value)
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}

func storeRelPath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func upsertSessionRef(values []SessionRef, next SessionRef) []SessionRef {
	for i := range values {
		if values[i].ID == next.ID {
			values[i] = next
			return values
		}
	}
	return append(values, next)
}
