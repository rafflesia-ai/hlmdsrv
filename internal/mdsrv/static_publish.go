package mdsrv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type StaticPublishReport struct {
	Store        string                     `json:"store"`
	Out          string                     `json:"out"`
	Files        []string                   `json:"files"`
	Verification *StaticPublishVerification `json:"verification,omitempty"`
}

type StaticPublishVerification struct {
	Root    string               `json:"root"`
	OK      bool                 `json:"ok"`
	Checks  []StaticPublishCheck `json:"checks"`
	Missing []string             `json:"missing,omitempty"`
	Errors  []string             `json:"errors,omitempty"`
}

type StaticPublishCheck struct {
	Kind  string `json:"kind"`
	Path  string `json:"path"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (s Store) PublishStatic(out string, force bool) (StaticPublishReport, error) {
	absoluteOut, err := filepath.Abs(out)
	if err != nil {
		return StaticPublishReport{}, err
	}
	relativeOut, err := filepath.Rel(s.Root, absoluteOut)
	if err != nil {
		return StaticPublishReport{}, err
	}
	if relativeOut == "." || (relativeOut != ".." && !strings.HasPrefix(relativeOut, ".."+string(filepath.Separator))) {
		return StaticPublishReport{}, fmt.Errorf("static publish output must be outside the source store")
	}
	report := StaticPublishReport{Store: s.Root, Out: absoluteOut}
	if err := os.MkdirAll(absoluteOut, 0o755); err != nil {
		return StaticPublishReport{}, err
	}
	for _, name := range []string{"trajectory_index.json", "session_index.json"} {
		source := filepath.Join(s.Root, name)
		if _, err := os.Stat(source); err == nil {
			if err := copyStaticFile(source, filepath.Join(absoluteOut, name), force); err != nil {
				return report, err
			}
			report.Files = append(report.Files, filepath.ToSlash(name))
		}
	}
	for _, dir := range []string{DatasetsDir, TopologyDir, TrajectoryDir, SessionDir, IndexesDir, ChunksDir, "traces", "visualization"} {
		sourceRoot := filepath.Join(s.Root, dir)
		if _, err := os.Stat(sourceRoot); err != nil {
			continue
		}
		err := filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return walkErr
			}
			// Skip hidden/AppleDouble "._*" files macOS leaves in a store on a
			// non-native filesystem so they are not copied into the clean,
			// read-only published output.
			if strings.HasPrefix(entry.Name(), ".") {
				return nil
			}
			relative, err := filepath.Rel(s.Root, path)
			if err != nil {
				return err
			}
			target := filepath.Join(absoluteOut, relative)
			if err := copyStaticFile(path, target, force); err != nil {
				return err
			}
			report.Files = append(report.Files, filepath.ToSlash(relative))
			return nil
		})
		if err != nil {
			return report, err
		}
	}
	return report, nil
}

func copyStaticFile(source, target string, force bool) error {
	if _, err := os.Stat(target); err == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", target)
	}
	_, _, err := copyWithChecksum(source, target, true)
	return err
}

func VerifyStaticPublish(root string) (StaticPublishVerification, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return StaticPublishVerification{}, err
	}
	verification := StaticPublishVerification{Root: absoluteRoot, OK: true}
	verifyStaticJSON[trajectoryIndexEntry](&verification, absoluteRoot, "catalog", "trajectory_index.json")
	verifyStaticJSON[sessionIndexEntry](&verification, absoluteRoot, "catalog", "session_index.json")

	var trajectoryEntries []trajectoryIndexEntry
	if err := readStaticJSON(filepath.Join(absoluteRoot, "trajectory_index.json"), &trajectoryEntries); err == nil {
		for _, entry := range trajectoryEntries {
			verifyStaticPath(&verification, absoluteRoot, "dataset_manifest", filepath.ToSlash(filepath.Join(DatasetsDir, entry.ID+".yaml")))
		}
	}
	var sessionEntries []sessionIndexEntry
	if err := readStaticJSON(filepath.Join(absoluteRoot, "session_index.json"), &sessionEntries); err == nil {
		for _, entry := range sessionEntries {
			matches, _ := filepath.Glob(filepath.Join(absoluteRoot, SessionDir, entry.ID+".*"))
			if len(matches) == 0 {
				verifyStaticMissing(&verification, "session", filepath.ToSlash(filepath.Join(SessionDir, entry.ID+".*")), "no session file matched session_index entry")
			} else {
				for _, match := range matches {
					relative, _ := filepath.Rel(absoluteRoot, match)
					verifyStaticPath(&verification, absoluteRoot, "session", filepath.ToSlash(relative))
				}
			}
		}
	}

	datasetRoot := filepath.Join(absoluteRoot, DatasetsDir)
	_ = filepath.WalkDir(datasetRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			verifyStaticError(&verification, "dataset_manifest", storeRelPath(absoluteRoot, path), walkErr)
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".yaml") {
			return nil
		}
		// Skip dotfiles for the same reason ListDatasets does: a macOS AppleDouble
		// "._<name>.yaml" sidecar is not a manifest we wrote, and failing on it made
		// publishing a valid store impossible on any non-native filesystem.
		if isHiddenSidecar(path) {
			return nil
		}
		relative := storeRelPath(absoluteRoot, path)
		verifyStaticPath(&verification, absoluteRoot, "dataset_manifest", relative)
		m, err := LoadManifestFile(path)
		if err != nil {
			verifyStaticError(&verification, "dataset_manifest", relative, err)
			return nil
		}
		verifyManifestStaticReferences(&verification, Store{Root: absoluteRoot}, m)
		return nil
	})
	return verification, nil
}

func verifyManifestStaticReferences(verification *StaticPublishVerification, store Store, m Manifest) {
	verifyFileRefPath(verification, store.Root, "topology", m.Inputs.Topology)
	for _, trajectory := range m.Inputs.Trajectories {
		verifyFileRefPath(verification, store.Root, "trajectory", trajectory)
	}
	for _, analysis := range m.Analyses {
		if analysis.Output != "" {
			verifyStaticPath(verification, store.Root, "trace", analysis.Output)
		}
	}
	if m.Streaming.FrameIndex != "" {
		verifyStaticPath(verification, store.Root, "frame_index", m.Streaming.FrameIndex)
		if index, err := store.LoadFrameIndex(m.Metadata.ID); err != nil {
			verifyStaticError(verification, "frame_index", m.Streaming.FrameIndex, err)
		} else {
			for _, chunk := range index.Chunks {
				if chunk.Path != "" {
					verifyStaticPath(verification, store.Root, "chunk", chunk.Path)
				}
			}
		}
	}
	if m.Visualization.MVS.Scene != "" {
		verifyStaticPath(verification, store.Root, "mvs", m.Visualization.MVS.Scene)
	}
	if m.Visualization.Molstar.State != "" {
		verifyStaticPath(verification, store.Root, "molstar_state", m.Visualization.Molstar.State)
	}
	for _, session := range m.Visualization.Sessions {
		if session.Path != "" {
			verifyStaticPath(verification, store.Root, "session", session.Path)
		}
	}
}

func verifyFileRefPath(verification *StaticPublishVerification, root string, kind string, ref FileRef) {
	if ref.Path != "" {
		verifyStaticPath(verification, root, kind, ref.Path)
	}
}

func verifyStaticJSON[T any](verification *StaticPublishVerification, root, kind, relative string) {
	path := filepath.Join(root, filepath.FromSlash(relative))
	var values []T
	if err := readStaticJSON(path, &values); err != nil {
		verifyStaticError(verification, kind, relative, err)
		return
	}
	verifyStaticOK(verification, kind, relative)
}

func readStaticJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(data)) == "" {
		return fmt.Errorf("empty JSON file")
	}
	return json.Unmarshal(data, value)
}

func verifyStaticPath(verification *StaticPublishVerification, root, kind, relative string) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	path := filepath.Join(root, clean)
	resolved, err := filepath.Abs(path)
	if err != nil {
		verifyStaticError(verification, kind, relative, err)
		return
	}
	if !strings.HasPrefix(resolved, root+string(filepath.Separator)) && resolved != root {
		verifyStaticMissing(verification, kind, relative, "path escapes static root")
		return
	}
	info, err := os.Stat(resolved)
	if err != nil {
		verifyStaticMissing(verification, kind, relative, err.Error())
		return
	}
	if info.IsDir() {
		verifyStaticMissing(verification, kind, relative, "is a directory")
		return
	}
	verifyStaticOK(verification, kind, relative)
}

func verifyStaticOK(verification *StaticPublishVerification, kind, relative string) {
	addStaticPublishCheck(verification, StaticPublishCheck{Kind: kind, Path: filepath.ToSlash(relative), OK: true})
}

func verifyStaticMissing(verification *StaticPublishVerification, kind, relative, message string) {
	verification.OK = false
	path := filepath.ToSlash(relative)
	if addStaticPublishCheck(verification, StaticPublishCheck{Kind: kind, Path: path, OK: false, Error: message}) {
		verification.Missing = append(verification.Missing, path)
	}
}

func verifyStaticError(verification *StaticPublishVerification, kind, relative string, err error) {
	verification.OK = false
	path := filepath.ToSlash(relative)
	if addStaticPublishCheck(verification, StaticPublishCheck{Kind: kind, Path: path, OK: false, Error: err.Error()}) {
		verification.Errors = append(verification.Errors, fmt.Sprintf("%s: %v", path, err))
	}
}

func addStaticPublishCheck(verification *StaticPublishVerification, check StaticPublishCheck) bool {
	check.Path = filepath.ToSlash(check.Path)
	for i, existing := range verification.Checks {
		if existing.Kind != check.Kind || existing.Path != check.Path {
			continue
		}
		if !check.OK && existing.OK {
			verification.Checks[i] = check
			return true
		}
		return false
	}
	verification.Checks = append(verification.Checks, check)
	return true
}
