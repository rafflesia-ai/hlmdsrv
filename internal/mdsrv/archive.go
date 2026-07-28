package mdsrv

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type PackReport struct {
	ID    string   `json:"id"`
	Path  string   `json:"path"`
	Files []string `json:"files"`
}

type UnpackReport struct {
	ID    string   `json:"id,omitempty"`
	Path  string   `json:"path"`
	Files []string `json:"files"`
}

func (s Store) PackDataset(id, out string, force bool) (PackReport, error) {
	if err := ValidateID(id); err != nil {
		return PackReport{}, fmt.Errorf("id: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		out = id + ".mdsrvx"
	}
	if _, err := os.Stat(out); err == nil && !force {
		return PackReport{}, fmt.Errorf("%s already exists; pass --force to overwrite", out)
	}
	m, err := s.LoadDataset(id)
	if err != nil {
		return PackReport{}, err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return PackReport{}, err
	}
	tmp := out + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return PackReport{}, err
	}
	zipWriter := zip.NewWriter(file)
	report := PackReport{ID: id, Path: out}
	added := map[string]bool{}

	if err := addManifestAsIndex(zipWriter, m); err != nil {
		_ = zipWriter.Close()
		_ = file.Close()
		_ = os.Remove(tmp)
		return PackReport{}, err
	}
	report.Files = append(report.Files, "index.yaml")
	added["index.yaml"] = true

	for _, path := range s.packPaths(m) {
		if added[path] {
			continue
		}
		if err := s.addStoreFile(zipWriter, path); err != nil {
			_ = zipWriter.Close()
			_ = file.Close()
			_ = os.Remove(tmp)
			return PackReport{}, err
		}
		report.Files = append(report.Files, path)
		added[path] = true
	}
	if err := zipWriter.Close(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return PackReport{}, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return PackReport{}, err
	}
	if err := os.Rename(tmp, out); err != nil {
		_ = os.Remove(tmp)
		return PackReport{}, err
	}
	return report, nil
}

func (s Store) UnpackArchive(path string, force bool) (UnpackReport, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		// A file that is not a zip is a caller-fixable input, but archive/zip's bare
		// "not a valid zip file" matched no classifier pattern and surfaced as
		// internal_error, whose documented meaning is "unclassified, report it".
		if !os.IsNotExist(err) {
			return UnpackReport{}, fmt.Errorf("%s is not a valid .mdsrvx archive: %w", path, err)
		}
		return UnpackReport{}, err
	}
	defer reader.Close()
	for _, dir := range []string{DatasetsDir, TopologyDir, TrajectoryDir, SessionDir, IndexesDir, ChunksDir, JobsDir, "visualization"} {
		if err := os.MkdirAll(filepath.Join(s.Root, dir), 0o755); err != nil {
			return UnpackReport{}, err
		}
	}
	if _, err := s.EnsureMetadata(); err != nil {
		return UnpackReport{}, err
	}
	report := UnpackReport{Path: path}
	var index Manifest
	for _, file := range reader.File {
		if file.Name == "index.yaml" {
			index, _ = readManifestFromZip(file)
			if index.Metadata.ID != "" {
				// The id becomes a filesystem path via ManifestPath; validate it
				// so a crafted archive cannot write its manifest outside the
				// store with an id like "../../etc/cron.d/evil".
				if err := ValidateID(index.Metadata.ID); err != nil {
					return UnpackReport{}, fmt.Errorf("archive manifest id %q is invalid: %w", index.Metadata.ID, err)
				}
				report.ID = index.Metadata.ID
			}
			break
		}
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.Name == "index.yaml" {
			report.Files = append(report.Files, file.Name)
			continue
		}
		if err := s.extractZipFile(file, force); err != nil {
			return UnpackReport{}, err
		}
		report.Files = append(report.Files, file.Name)
	}
	if index.Metadata.ID != "" {
		datasetPath := filepath.ToSlash(filepath.Join(DatasetsDir, index.Metadata.ID+".yaml"))
		if !containsString(report.Files, datasetPath) {
			if err := WriteManifestFile(s.ManifestPath(index.Metadata.ID), index); err != nil {
				return UnpackReport{}, err
			}
			report.Files = append(report.Files, datasetPath)
		}
	}
	if err := ensureJSONArray(filepath.Join(s.Root, "trajectory_index.json")); err != nil {
		return UnpackReport{}, err
	}
	if err := ensureJSONArray(filepath.Join(s.Root, "session_index.json")); err != nil {
		return UnpackReport{}, err
	}
	return report, nil
}

func (s Store) packPaths(m Manifest) []string {
	paths := []string{
		filepath.ToSlash(filepath.Join(DatasetsDir, m.Metadata.ID+".yaml")),
		"trajectory_index.json",
		"session_index.json",
	}
	if m.Inputs.Topology.Path != "" {
		paths = append(paths, m.Inputs.Topology.Path)
	}
	for _, trajectory := range m.Inputs.Trajectories {
		if trajectory.Path != "" {
			paths = append(paths, trajectory.Path)
		}
	}
	for _, analysis := range m.Analyses {
		if analysis.Output != "" {
			paths = append(paths, analysis.Output)
		}
	}
	if m.Streaming.FrameIndex != "" {
		paths = append(paths, m.Streaming.FrameIndex)
		if index, err := s.LoadFrameIndex(m.Metadata.ID); err == nil {
			for _, chunk := range index.Chunks {
				if chunk.Path != "" {
					paths = append(paths, chunk.Path)
				}
			}
		}
	}
	if m.Visualization.Molstar.State != "" {
		paths = append(paths, m.Visualization.Molstar.State)
	}
	if m.Visualization.MVS.Scene != "" {
		paths = append(paths, m.Visualization.MVS.Scene)
	}
	for _, session := range m.Visualization.Sessions {
		if session.Path != "" {
			paths = append(paths, session.Path)
		}
	}
	return paths
}

func addManifestAsIndex(zipWriter *zip.Writer, m Manifest) error {
	writer, err := zipWriter.Create("index.yaml")
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func (s Store) addStoreFile(zipWriter *zip.Writer, relativePath string) error {
	path, err := s.SafeResolvePath(relativePath)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", relativePath)
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(relativePath)
	header.Method = zip.Deflate
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}

func readManifestFromZip(file *zip.File) (Manifest, error) {
	reader, err := file.Open()
	if err != nil {
		return Manifest{}, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return Manifest{}, err
	}
	return DecodeManifest(data, file.Name)
}

func (s Store) extractZipFile(file *zip.File, force bool) error {
	target, err := s.SafeResolvePath(file.Name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	tmp := target + ".tmp"
	output, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, reader)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
