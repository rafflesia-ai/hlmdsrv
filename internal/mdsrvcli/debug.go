package mdsrvcli

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/gromacs"
	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type debugBundleFlags struct {
	store       string
	out         string
	backend     string
	gmxCommand  string
	deep        bool
	strict      bool
	skipSmoke   bool
	maxLogs     int
	logBytes    int64
	maxFileSize int64
	jsonReport  bool
}

type debugBundleReport struct {
	OK        bool               `json:"ok"`
	DatasetID string             `json:"dataset_id"`
	Store     string             `json:"store"`
	Path      string             `json:"path"`
	CreatedAt string             `json:"created_at"`
	Files     []string           `json:"files"`
	Errors    []debugBundleError `json:"errors,omitempty"`
	Warnings  []string           `json:"warnings,omitempty"`
}

type debugBundleError struct {
	Component string `json:"component"`
	Error     string `json:"error"`
}

type debugContextReport struct {
	Executable string            `json:"executable,omitempty"`
	Args       []string          `json:"args,omitempty"`
	CWD        string            `json:"cwd,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}

type debugBackendReport struct {
	Python         *mdsrv.BackendDoctor `json:"python,omitempty"`
	PythonError    string               `json:"python_error,omitempty"`
	GromacsCommand string               `json:"gromacs_command"`
	GromacsOK      bool                 `json:"gromacs_ok"`
	GromacsVersion string               `json:"gromacs_version,omitempty"`
	GromacsError   string               `json:"gromacs_error,omitempty"`
}

type debugFrameIndexSummary struct {
	OK              bool              `json:"ok"`
	DatasetID       string            `json:"dataset_id,omitempty"`
	FrameCount      int               `json:"frame_count,omitempty"`
	AtomCount       int               `json:"atom_count,omitempty"`
	TimeStart       float64           `json:"time_start,omitempty"`
	TimeEnd         float64           `json:"time_end,omitempty"`
	TimeStep        float64           `json:"time_step,omitempty"`
	ChunkSizeFrames int               `json:"chunk_size_frames,omitempty"`
	ChunkCount      int               `json:"chunk_count,omitempty"`
	ChunkEncodings  map[string]int    `json:"chunk_encodings,omitempty"`
	Materialized    bool              `json:"materialized"`
	FirstFrame      *mdsrv.FramePoint `json:"first_frame,omitempty"`
	LastFrame       *mdsrv.FramePoint `json:"last_frame,omitempty"`
	FirstChunk      *mdsrv.FrameChunk `json:"first_chunk,omitempty"`
	LastChunk       *mdsrv.FrameChunk `json:"last_chunk,omitempty"`
	Error           string            `json:"error,omitempty"`
}

type debugStoreFile struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (a app) debugCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Collect MDsrv headless diagnostics",
	}
	cmd.AddCommand(a.debugBundleCommand())
	return cmd
}

func (a app) debugBundleCommand() *cobra.Command {
	flags := &debugBundleFlags{store: "./mdsrv-data", maxLogs: 5, logBytes: 64 * 1024, maxFileSize: 2 * 1024 * 1024}
	cmd := &cobra.Command{
		Use:   "bundle DATASET_ID",
		Short: "Write a small zip archive with store, backend, validation, and server diagnostics",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := a.runDebugBundle(cmd.Context(), args[0], flags)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "debug bundle %s\n", report.Path)
			if !report.OK {
				fmt.Fprintf(a.stderr, "warning: debug bundle captured %d diagnostic error(s)\n", len(report.Errors))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", flags.store, "MDsrv store root")
	cmd.Flags().StringVarP(&flags.out, "out", "o", "", "output zip path; defaults to DATASET_ID-debug-bundle.zip")
	bindBackendFlag(cmd, &flags.backend)
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	cmd.Flags().BoolVar(&flags.deep, "deep", false, "decode trajectory metadata during validation when a Python backend is available")
	cmd.Flags().BoolVar(&flags.strict, "strict", false, "treat optional missing artifacts and unavailable requested backends as validation errors")
	cmd.Flags().BoolVar(&flags.skipSmoke, "skip-smoke", false, "skip in-process HTTP serve smoke diagnostics")
	cmd.Flags().IntVar(&flags.maxLogs, "max-logs", flags.maxLogs, "maximum recent job log directories to include")
	cmd.Flags().Int64Var(&flags.logBytes, "log-bytes", flags.logBytes, "maximum bytes kept from each job.log")
	cmd.Flags().Int64Var(&flags.maxFileSize, "max-file-bytes", flags.maxFileSize, "maximum store metadata file bytes copied into the bundle")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) runDebugBundle(ctx context.Context, datasetID string, flags *debugBundleFlags) (debugBundleReport, error) {
	store, err := mdsrv.OpenStore(flags.store)
	if err != nil {
		return debugBundleReport{}, err
	}
	if err := store.Init(); err != nil {
		return debugBundleReport{}, err
	}
	manifest, err := store.LoadDataset(datasetID)
	if err != nil {
		return debugBundleReport{}, err
	}
	out := strings.TrimSpace(flags.out)
	if out == "" {
		out = datasetID + "-debug-bundle.zip"
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return debugBundleReport{}, err
	}
	// The bundle is staged in a temp file and renamed into place, so a FIFO target
	// would be silently unlinked and replaced rather than written through, and an
	// --out naming one of the dataset's own files was overwritten at exit 0.
	if err := rejectNonRegularOutput(out); err != nil {
		return debugBundleReport{}, err
	}
	if err := rejectDatasetInputOverwrite(store, manifest, out); err != nil {
		return debugBundleReport{}, err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return debugBundleReport{}, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(out), "."+filepath.Base(out)+".*.tmp")
	if err != nil {
		return debugBundleReport{}, err
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		_ = tmp.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	report := debugBundleReport{
		OK:        true,
		DatasetID: datasetID,
		Store:     store.Root,
		Path:      out,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	zipWriter := zip.NewWriter(tmp)
	addError := func(component string, err error) {
		if err == nil {
			return
		}
		report.OK = false
		report.Errors = append(report.Errors, debugBundleError{Component: component, Error: err.Error()})
	}
	addBytes := func(name string, data []byte) {
		name = filepath.ToSlash(strings.TrimPrefix(filepath.Clean(name), string(filepath.Separator)))
		writer, err := zipWriter.Create(name)
		if err != nil {
			addError(name, err)
			return
		}
		if _, err := writer.Write(data); err != nil {
			addError(name, err)
			return
		}
		report.Files = append(report.Files, name)
	}
	addJSON := func(name string, value any) {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			addError(name, err)
			return
		}
		addBytes(name, append(data, '\n'))
	}
	addStoreFile := func(name string, path string) {
		data, err := readSmallFile(path, flags.maxFileSize)
		if err != nil {
			addError(name, err)
			return
		}
		addBytes(name, data)
	}

	addJSON("context.json", debugContext())
	addJSON("manifest.json", manifest)
	addStoreFile("store/datasets/"+datasetID+".yaml", store.ManifestPath(datasetID))
	for _, path := range []string{"trajectory_index.json", "session_index.json"} {
		addStoreFile("store/"+path, filepath.Join(store.Root, path))
	}
	addJSON("store_listing.json", collectDebugStoreListing(store, datasetID))

	doctor := a.runDoctor(ctx, &doctorFlags{store: store.Root, gmxCommand: flags.gmxCommand})
	addJSON("doctor.json", doctor)
	addJSON("backend.json", debugBackend(ctx, store, flags))

	validation := datasetValidationReport{OK: false}
	if validationReport, err := buildDatasetValidationReport(ctx, store, manifest, store.Root, &validateFlags{
		store:      store.Root,
		deep:       flags.deep,
		strict:     flags.strict,
		backend:    flags.backend,
		gmxCommand: flags.gmxCommand,
	}); err != nil {
		addError("validation", err)
	} else {
		validation = validationReport
		if !validation.OK {
			addError("validation", fmt.Errorf("validation failed"))
		}
	}
	addJSON("validation.json", validation)

	frameIndexSummary, frameIndex, frameIndexErr := debugFrameIndex(store, datasetID)
	if frameIndexErr != nil {
		addError("frame_index", frameIndexErr)
	}
	addJSON("frame_index_summary.json", frameIndexSummary)
	if frameIndexErr == nil {
		if indexPath := manifest.Streaming.FrameIndex; indexPath != "" {
			if resolved, err := store.SafeResolvePath(indexPath); err == nil {
				addStoreFile("store/"+indexPath, resolved)
			}
		}
		if len(frameIndex.Chunks) > 0 && frameIndex.Chunks[0].Path != "" {
			report.Warnings = append(report.Warnings, "frame chunks are summarized but chunk payloads are not copied")
		}
	}

	if !flags.skipSmoke {
		smokeFlags := &serveFlags{
			store:         store.Root,
			backend:       flags.backend,
			gmxCommand:    flags.gmxCommand,
			readOnly:      true,
			maxFrameRange: 256,
		}
		smoke, err := a.runServeSmoke(ctx, store, smokeFlags)
		if err != nil {
			addError("serve_smoke", err)
		}
		addJSON("serve_smoke.json", smoke)
	}

	addRecentJobLogs(store, zipWriter, &report, flags, addError)
	addJSON("summary.json", report)
	sort.Strings(report.Files)

	if err := zipWriter.Close(); err != nil {
		return debugBundleReport{}, err
	}
	if err := tmp.Close(); err != nil {
		return debugBundleReport{}, err
	}
	if err := os.Rename(tmpPath, out); err != nil {
		return debugBundleReport{}, err
	}
	success = true
	return report, nil
}

func debugContext() debugContextReport {
	cwd, _ := os.Getwd()
	env := map[string]string{}
	for _, key := range []string{"MDSRV_GMX", "MDSRV_PYTHON", "MDSRV_PROFILE", "MDSRV_AUTH_TOKEN"} {
		if value, ok := os.LookupEnv(key); ok {
			env[key] = redactEnv(key, value)
		}
	}
	return debugContextReport{
		Executable: firstArg(os.Args),
		Args:       append([]string(nil), os.Args...),
		CWD:        cwd,
		Env:        env,
	}
}

func debugBackend(ctx context.Context, store mdsrv.Store, flags *debugBundleFlags) debugBackendReport {
	report := debugBackendReport{}
	backend := mdsrv.NewBackend(store)
	if doctor, err := backend.Doctor(ctx); err == nil {
		report.Python = &doctor
	} else {
		report.PythonError = err.Error()
	}
	gmx := gromacs.New(gromacs.Options{Command: flags.gmxCommand})
	report.GromacsCommand = gmx.CommandString()
	report.GromacsOK = gmx.Available()
	if report.GromacsOK {
		if version, err := gmx.Version(ctx); err == nil {
			report.GromacsVersion = version
		} else {
			report.GromacsError = err.Error()
		}
	}
	return report
}

func debugFrameIndex(store mdsrv.Store, datasetID string) (debugFrameIndexSummary, mdsrv.FrameIndex, error) {
	index, err := store.LoadFrameIndex(datasetID)
	if err != nil {
		return debugFrameIndexSummary{OK: false, DatasetID: datasetID, Error: err.Error()}, mdsrv.FrameIndex{}, err
	}
	summary := debugFrameIndexSummary{
		OK:              true,
		DatasetID:       index.DatasetID,
		FrameCount:      index.FrameCount,
		AtomCount:       index.AtomCount,
		TimeStart:       index.TimeStart,
		TimeEnd:         index.TimeEnd,
		TimeStep:        index.TimeStep,
		ChunkSizeFrames: index.ChunkSizeFrames,
		ChunkCount:      len(index.Chunks),
		ChunkEncodings:  map[string]int{},
	}
	if len(index.Frames) > 0 {
		first := index.Frames[0]
		last := index.Frames[len(index.Frames)-1]
		summary.FirstFrame = &first
		summary.LastFrame = &last
	}
	if len(index.Chunks) > 0 {
		first := index.Chunks[0]
		last := index.Chunks[len(index.Chunks)-1]
		summary.FirstChunk = &first
		summary.LastChunk = &last
		for _, chunk := range index.Chunks {
			encoding := firstNonEmpty(chunk.Encoding, "json")
			summary.ChunkEncodings[encoding]++
			if chunk.Path != "" {
				summary.Materialized = true
			}
		}
	}
	if len(summary.ChunkEncodings) == 0 {
		summary.ChunkEncodings = nil
	}
	return summary, index, nil
}

func collectDebugStoreListing(store mdsrv.Store, datasetID string) []debugStoreFile {
	var files []debugStoreFile
	interesting := []string{
		"trajectory_index.json",
		"session_index.json",
		filepath.ToSlash(filepath.Join(mdsrv.DatasetsDir, datasetID+".yaml")),
		filepath.ToSlash(filepath.Join(mdsrv.IndexesDir, datasetID+"-frame-index.json")),
	}
	for _, rel := range interesting {
		path := filepath.Join(store.Root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		item := debugStoreFile{Path: rel}
		if err != nil {
			item.Error = err.Error()
			files = append(files, item)
			continue
		}
		item.Bytes = info.Size()
		if !info.IsDir() {
			item.SHA256 = fileSHA256(path)
		}
		files = append(files, item)
	}
	return files
}

func addRecentJobLogs(store mdsrv.Store, zipWriter *zip.Writer, report *debugBundleReport, flags *debugBundleFlags, addError func(string, error)) {
	jobsRoot := filepath.Join(store.Root, mdsrv.JobsDir)
	entries, err := os.ReadDir(jobsRoot)
	if err != nil {
		if !os.IsNotExist(err) {
			addError("jobs", err)
		}
		return
	}
	type jobEntry struct {
		name    string
		modTime time.Time
	}
	var jobs []jobEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		jobs = append(jobs, jobEntry{name: entry.Name(), modTime: info.ModTime()})
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].modTime.After(jobs[j].modTime)
	})
	limit := flags.maxLogs
	if limit < 0 {
		limit = 0
	}
	if limit > len(jobs) {
		limit = len(jobs)
	}
	for _, job := range jobs[:limit] {
		for _, name := range []string{"status.json", "job.log"} {
			path := filepath.Join(jobsRoot, job.name, name)
			var data []byte
			var readErr error
			if name == "job.log" {
				data, readErr = tailFile(path, flags.logBytes)
			} else {
				data, readErr = readSmallFile(path, flags.maxFileSize)
			}
			if readErr != nil {
				addError("jobs/"+job.name+"/"+name, readErr)
				continue
			}
			writer, err := zipWriter.Create(filepath.ToSlash(filepath.Join("jobs", job.name, name)))
			if err != nil {
				addError("jobs/"+job.name+"/"+name, err)
				continue
			}
			if _, err := writer.Write(data); err != nil {
				addError("jobs/"+job.name+"/"+name, err)
				continue
			}
			report.Files = append(report.Files, filepath.ToSlash(filepath.Join("jobs", job.name, name)))
		}
	}
}

func readSmallFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return nil, fmt.Errorf("%s is %d bytes, above --max-file-bytes=%d", path, info.Size(), maxBytes)
	}
	return os.ReadFile(path)
}

func tailFile(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return []byte{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	start := int64(0)
	if info.Size() > maxBytes {
		start = info.Size() - maxBytes
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if start > 0 {
		buf.WriteString("[truncated]\n")
	}
	if _, err := io.Copy(&buf, file); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fileSHA256(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func redactEnv(key string, value string) string {
	upper := strings.ToUpper(key)
	if strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASS") {
		return "<redacted>"
	}
	return value
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
