package mdsrvcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type serverJobRequest struct {
	Type           string                `json:"type"`
	DatasetID      string                `json:"dataset_id"`
	Backend        string                `json:"backend,omitempty"`
	ChunkSize      int                   `json:"chunk_size,omitempty"`
	Encoding       string                `json:"encoding,omitempty"`
	Force          bool                  `json:"force,omitempty"`
	Analysis       mdsrv.AnalysisRequest `json:"analysis,omitempty"`
	TimeoutSeconds int                   `json:"timeout_seconds,omitempty"`
}

type serverJobStatus struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	DatasetID  string           `json:"dataset_id"`
	Status     string           `json:"status"`
	CreatedAt  time.Time        `json:"created_at"`
	StartedAt  *time.Time       `json:"started_at,omitempty"`
	FinishedAt *time.Time       `json:"finished_at,omitempty"`
	Error      string           `json:"error,omitempty"`
	Request    serverJobRequest `json:"request"`
	Result     map[string]any   `json:"result,omitempty"`
}

type serverJobRecord struct {
	serverJobStatus
	cancel context.CancelFunc
}

const jobEventVersion = "mdsrv.job_event/v1"

type jobEventType string

const (
	jobEventSubmitted         jobEventType = "submitted"
	jobEventStarted           jobEventType = "started"
	jobEventChunksStarted     jobEventType = "chunks_started"
	jobEventChunksCompleted   jobEventType = "chunks_completed"
	jobEventAnalysisStarted   jobEventType = "analysis_started"
	jobEventAnalysisCompleted jobEventType = "analysis_completed"
	jobEventSucceeded         jobEventType = "succeeded"
	jobEventFailed            jobEventType = "failed"
	jobEventCanceled          jobEventType = "canceled"
	jobEventRetried           jobEventType = "retried"
)

type serverJobLog struct {
	ID  string `json:"id"`
	Log string `json:"log"`
}

type serverJobEvent struct {
	Version string         `json:"version"`
	At      time.Time      `json:"at"`
	ID      string         `json:"id"`
	Status  string         `json:"status,omitempty"`
	Type    jobEventType   `json:"type"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type serverJobStats struct {
	Workers                int            `json:"workers"`
	MaxQueue               int            `json:"max_queue"`
	QueuedChannelDepth     int            `json:"queued_channel_depth"`
	Total                  int            `json:"total"`
	Counts                 map[string]int `json:"counts"`
	OldestQueuedAt         *time.Time     `json:"oldest_queued_at,omitempty"`
	OldestQueuedAgeSeconds float64        `json:"oldest_queued_age_seconds,omitempty"`
}

type jobPruneOptions struct {
	Store  string
	TTL    time.Duration
	Status map[string]bool
	DryRun bool
}

type jobPruneReport struct {
	Store       string   `json:"store"`
	DryRun      bool     `json:"dry_run"`
	TTLSeconds  float64  `json:"ttl_seconds"`
	Statuses    []string `json:"statuses,omitempty"`
	Removed     []string `json:"removed,omitempty"`
	WouldRemove []string `json:"would_remove,omitempty"`
	Kept        []string `json:"kept,omitempty"`
	Errors      []string `json:"errors,omitempty"`
}

type serverJobQueue struct {
	store   mdsrv.Store
	options serverOptions
	queue   chan string
	next    atomic.Uint64

	mu   sync.Mutex
	jobs map[string]*serverJobRecord
}

func newServerJobQueue(store mdsrv.Store, options serverOptions) *serverJobQueue {
	if options.Workers <= 0 {
		return nil
	}
	if options.MaxQueue < 1 {
		options.MaxQueue = 64
	}
	queue := &serverJobQueue{
		store:   store,
		options: options,
		queue:   make(chan string, options.MaxQueue),
		jobs:    make(map[string]*serverJobRecord),
	}
	queue.loadPersistedJobs()
	for worker := 0; worker < options.Workers; worker++ {
		go queue.worker()
	}
	return queue
}

func (q *serverJobQueue) handleCollection(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/jobs" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeHTTPJSON(w, http.StatusOK, q.list())
	case http.MethodPost:
		if q.options.ReadOnly {
			writeHTTPError(w, http.StatusForbidden, errors.New("server is read-only"))
			return
		}
		var request serverJobRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeHTTPError(w, http.StatusBadRequest, err)
			return
		}
		status, err := q.submit(request)
		if err != nil {
			statusCode := http.StatusBadRequest
			if errors.Is(err, errJobQueueFull) {
				statusCode = http.StatusTooManyRequests
			}
			writeHTTPError(w, statusCode, err)
			return
		}
		writeHTTPJSON(w, http.StatusAccepted, status)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (q *serverJobQueue) handleItem(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/jobs/"), "/"), "/")
	if len(parts) == 0 || parts[0] == "" || len(parts) > 2 {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 1 && parts[0] == "stats" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeHTTPJSON(w, http.StatusOK, q.stats())
		return
	}
	id := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	switch action {
	case "":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		q.handleJobStatus(w, id)
	case "logs":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		q.handleJobLogs(w, r, id)
	case "events":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		q.handleJobEvents(w, r, id)
	case "cancel":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if q.options.ReadOnly {
			writeHTTPError(w, http.StatusForbidden, errors.New("server is read-only"))
			return
		}
		status, err := q.cancel(id)
		if err != nil {
			writeHTTPError(w, http.StatusNotFound, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, status)
	case "retry":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if q.options.ReadOnly {
			writeHTTPError(w, http.StatusForbidden, errors.New("server is read-only"))
			return
		}
		status, err := q.retry(id)
		if err != nil {
			statusCode := http.StatusBadRequest
			if strings.Contains(err.Error(), "not found") {
				statusCode = http.StatusNotFound
			}
			if errors.Is(err, errJobQueueFull) {
				statusCode = http.StatusTooManyRequests
			}
			writeHTTPError(w, statusCode, err)
			return
		}
		writeHTTPJSON(w, http.StatusAccepted, status)
	default:
		http.NotFound(w, r)
	}
}

func (q *serverJobQueue) handleJobStatus(w http.ResponseWriter, id string) {
	status, ok := q.get(id)
	if !ok {
		writeHTTPError(w, http.StatusNotFound, fmt.Errorf("job %q not found", id))
		return
	}
	writeHTTPJSON(w, http.StatusOK, status)
}

func (q *serverJobQueue) handleJobEvents(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := q.get(id); !ok {
		writeHTTPError(w, http.StatusNotFound, fmt.Errorf("job %q not found", id))
		return
	}
	events, raw, err := q.readEvents(id)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	if r.URL.Query().Get("format") == "json" {
		writeHTTPJSON(w, http.StatusOK, map[string]any{"id": id, "events": events})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (q *serverJobQueue) handleJobLogs(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := q.get(id); !ok {
		writeHTTPError(w, http.StatusNotFound, fmt.Errorf("job %q not found", id))
		return
	}
	log, err := q.readLog(id)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	if r.URL.Query().Get("format") == "json" {
		writeHTTPJSON(w, http.StatusOK, serverJobLog{ID: id, Log: log})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(log))
}

var errJobQueueFull = errors.New("job queue is full")

func (q *serverJobQueue) submit(request serverJobRequest) (serverJobStatus, error) {
	request.Type = strings.ToLower(strings.TrimSpace(request.Type))
	request.DatasetID = strings.TrimSpace(request.DatasetID)
	if request.DatasetID == "" {
		return serverJobStatus{}, errors.New("dataset_id is required")
	}
	if _, err := q.store.LoadDataset(request.DatasetID); err != nil {
		return serverJobStatus{}, err
	}
	switch request.Type {
	case "chunks":
		if request.Encoding == "" {
			request.Encoding = "json"
		}
		if _, err := mdsrv.NormalizeFrameChunkEncoding(request.Encoding); err != nil {
			return serverJobStatus{}, err
		}
	case "analysis":
		if strings.TrimSpace(request.Analysis.Type) == "" {
			return serverJobStatus{}, errors.New("analysis.type is required")
		}
	default:
		return serverJobStatus{}, fmt.Errorf("unsupported job type %q", request.Type)
	}
	if request.TimeoutSeconds < 0 {
		return serverJobStatus{}, errors.New("timeout_seconds cannot be negative")
	}

	id := fmt.Sprintf("job_%d_%06d", time.Now().UTC().UnixNano(), q.next.Add(1))
	status := serverJobStatus{
		ID:        id,
		Type:      request.Type,
		DatasetID: request.DatasetID,
		Status:    "queued",
		CreatedAt: time.Now().UTC(),
		Request:   request,
	}
	q.mu.Lock()
	q.jobs[id] = &serverJobRecord{serverJobStatus: status}
	q.persistStatusLocked(q.jobs[id])
	q.mu.Unlock()
	q.appendLog(id, jobEventSubmitted, map[string]any{"type": request.Type, "dataset_id": request.DatasetID}, "submitted type=%s dataset=%s", request.Type, request.DatasetID)

	select {
	case q.queue <- id:
		return status, nil
	default:
		q.mu.Lock()
		delete(q.jobs, id)
		q.mu.Unlock()
		_ = os.RemoveAll(q.jobDir(id))
		return serverJobStatus{}, errJobQueueFull
	}
}

func (q *serverJobQueue) list() []serverJobStatus {
	q.mu.Lock()
	defer q.mu.Unlock()
	statuses := make([]serverJobStatus, 0, len(q.jobs))
	for _, job := range q.jobs {
		statuses = append(statuses, job.serverJobStatus)
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].CreatedAt.Before(statuses[j].CreatedAt)
	})
	return statuses
}

func (q *serverJobQueue) stats() serverJobStats {
	now := time.Now().UTC()
	q.mu.Lock()
	defer q.mu.Unlock()
	stats := serverJobStats{
		Workers:            q.options.Workers,
		MaxQueue:           q.options.MaxQueue,
		QueuedChannelDepth: len(q.queue),
		Counts:             map[string]int{},
	}
	for _, job := range q.jobs {
		stats.Total++
		stats.Counts[job.Status]++
		if job.Status == "queued" && (stats.OldestQueuedAt == nil || job.CreatedAt.Before(*stats.OldestQueuedAt)) {
			queuedAt := job.CreatedAt
			stats.OldestQueuedAt = &queuedAt
		}
	}
	if stats.OldestQueuedAt != nil {
		stats.OldestQueuedAgeSeconds = now.Sub(*stats.OldestQueuedAt).Seconds()
	}
	return stats
}

func jobMetricsText(stats serverJobStats, enabled bool) string {
	var builder strings.Builder
	writeMetricHelp := func(name string, metricType string, help string) {
		fmt.Fprintf(&builder, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
	}
	enabledValue := 0
	if enabled {
		enabledValue = 1
	}
	writeMetricHelp("mdsrv_job_queue_enabled", "gauge", "Whether the async job queue is enabled.")
	fmt.Fprintf(&builder, "mdsrv_job_queue_enabled %d\n", enabledValue)
	writeMetricHelp("mdsrv_job_workers", "gauge", "Configured async job worker count.")
	fmt.Fprintf(&builder, "mdsrv_job_workers %d\n", stats.Workers)
	writeMetricHelp("mdsrv_job_queue_capacity", "gauge", "Configured async job queue capacity.")
	fmt.Fprintf(&builder, "mdsrv_job_queue_capacity %d\n", stats.MaxQueue)
	writeMetricHelp("mdsrv_job_queue_depth", "gauge", "Current async job channel depth.")
	fmt.Fprintf(&builder, "mdsrv_job_queue_depth %d\n", stats.QueuedChannelDepth)
	writeMetricHelp("mdsrv_jobs_total", "gauge", "Current number of persisted jobs.")
	fmt.Fprintf(&builder, "mdsrv_jobs_total %d\n", stats.Total)
	writeMetricHelp("mdsrv_jobs_by_status", "gauge", "Current number of persisted jobs by status.")
	for _, status := range []string{"queued", "running", "succeeded", "failed", "canceled"} {
		fmt.Fprintf(&builder, "mdsrv_jobs_by_status{status=%q} %d\n", status, stats.Counts[status])
	}
	writeMetricHelp("mdsrv_job_oldest_queued_age_seconds", "gauge", "Age in seconds of the oldest queued job.")
	fmt.Fprintf(&builder, "mdsrv_job_oldest_queued_age_seconds %.3f\n", stats.OldestQueuedAgeSeconds)
	return builder.String()
}

func (q *serverJobQueue) get(id string) (serverJobStatus, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	job, ok := q.jobs[id]
	if !ok {
		return serverJobStatus{}, false
	}
	return job.serverJobStatus, true
}

func (q *serverJobQueue) retry(id string) (serverJobStatus, error) {
	q.mu.Lock()
	job, ok := q.jobs[id]
	if !ok {
		q.mu.Unlock()
		return serverJobStatus{}, fmt.Errorf("job %q not found", id)
	}
	if !isTerminalJobStatus(job.Status) {
		status := job.Status
		q.mu.Unlock()
		return serverJobStatus{}, fmt.Errorf("job %q is %s; only terminal jobs can be retried", id, status)
	}
	request := job.Request
	q.mu.Unlock()
	status, err := q.submit(request)
	if err != nil {
		return serverJobStatus{}, err
	}
	q.appendLog(id, jobEventRetried, map[string]any{"new_job_id": status.ID}, "retried as %s", status.ID)
	return status, nil
}

func pruneJobs(options jobPruneOptions) (jobPruneReport, error) {
	store, err := mdsrv.OpenStore(options.Store)
	if err != nil {
		return jobPruneReport{}, err
	}
	report := jobPruneReport{
		Store:      store.Root,
		DryRun:     options.DryRun,
		TTLSeconds: options.TTL.Seconds(),
	}
	for status := range options.Status {
		report.Statuses = append(report.Statuses, status)
	}
	sort.Strings(report.Statuses)
	jobsRoot := filepath.Join(store.Root, mdsrv.JobsDir)
	entries, err := os.ReadDir(jobsRoot)
	if errors.Is(err, os.ErrNotExist) {
		return report, nil
	}
	if err != nil {
		return jobPruneReport{}, err
	}
	cutoff := time.Now().UTC().Add(-options.TTL)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		statusPath := filepath.Join(jobsRoot, id, "status.json")
		raw, err := os.ReadFile(statusPath)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		var status serverJobStatus
		if err := json.Unmarshal(raw, &status); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		if status.ID == "" {
			status.ID = id
		}
		if len(options.Status) > 0 && !options.Status[status.Status] {
			report.Kept = append(report.Kept, status.ID)
			continue
		}
		timestamp := status.CreatedAt
		if status.FinishedAt != nil {
			timestamp = *status.FinishedAt
		}
		if options.TTL > 0 && timestamp.After(cutoff) {
			report.Kept = append(report.Kept, status.ID)
			continue
		}
		if options.DryRun {
			report.WouldRemove = append(report.WouldRemove, status.ID)
			continue
		}
		if err := os.RemoveAll(filepath.Join(jobsRoot, id)); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		report.Removed = append(report.Removed, status.ID)
	}
	sort.Strings(report.Kept)
	sort.Strings(report.Removed)
	sort.Strings(report.WouldRemove)
	return report, nil
}

func (q *serverJobQueue) cancel(id string) (serverJobStatus, error) {
	now := time.Now().UTC()
	q.mu.Lock()
	job, ok := q.jobs[id]
	if !ok {
		q.mu.Unlock()
		return serverJobStatus{}, fmt.Errorf("job %q not found", id)
	}
	switch job.Status {
	case "succeeded", "failed", "canceled":
		status := job.serverJobStatus
		q.mu.Unlock()
		return status, nil
	}
	job.Status = "canceled"
	job.FinishedAt = &now
	job.Error = "job canceled"
	cancel := job.cancel
	q.persistStatusLocked(job)
	status := job.serverJobStatus
	q.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	q.appendLog(id, jobEventCanceled, nil, "canceled")
	return status, nil
}

func (q *serverJobQueue) worker() {
	for id := range q.queue {
		q.run(id)
	}
}

func (q *serverJobQueue) run(id string) {
	request, ok := q.markRunning(id)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	timeout := q.options.JobTimeout
	if request.TimeoutSeconds > 0 {
		timeout = time.Duration(request.TimeoutSeconds) * time.Second
	}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	q.setCancel(id, cancel)

	result, err := q.executeGuarded(ctx, id, request)
	if err != nil {
		q.markFailed(id, err)
		return
	}
	q.markSucceeded(id, result)
}

// executeGuarded runs the job body and converts a panic into an ordinary job
// failure so a single malformed job (e.g. a crafted manifest that trips a
// bounds check deep in analysis) cannot crash the whole server process.
func (q *serverJobQueue) executeGuarded(ctx context.Context, id string, request serverJobRequest) (result map[string]any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("job panicked: %v", r)
		}
	}()
	return q.execute(ctx, id, request)
}

func (q *serverJobQueue) markRunning(id string) (serverJobRequest, bool) {
	now := time.Now().UTC()
	q.mu.Lock()
	defer q.mu.Unlock()
	job, ok := q.jobs[id]
	if !ok {
		return serverJobRequest{}, false
	}
	if job.Status == "canceled" {
		return serverJobRequest{}, false
	}
	job.Status = "running"
	job.StartedAt = &now
	q.persistStatusLocked(job)
	q.appendLogLocked(id, jobEventStarted, nil, "started")
	return job.Request, true
}

func (q *serverJobQueue) setCancel(id string, cancel context.CancelFunc) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if job, ok := q.jobs[id]; ok {
		job.cancel = cancel
	}
}

func (q *serverJobQueue) markFailed(id string, err error) {
	now := time.Now().UTC()
	q.mu.Lock()
	defer q.mu.Unlock()
	if job, ok := q.jobs[id]; ok {
		if job.Status == "canceled" {
			job.cancel = nil
			q.persistStatusLocked(job)
			q.appendLogLocked(id, jobEventCanceled, nil, "finished status=canceled")
			return
		}
		job.Status = "failed"
		job.FinishedAt = &now
		job.Error = err.Error()
		job.cancel = nil
		q.persistStatusLocked(job)
		q.appendLogLocked(id, jobEventFailed, map[string]any{"error": err.Error()}, "failed error=%q", err.Error())
	}
}

func (q *serverJobQueue) markSucceeded(id string, result map[string]any) {
	now := time.Now().UTC()
	q.mu.Lock()
	defer q.mu.Unlock()
	if job, ok := q.jobs[id]; ok {
		job.Status = "succeeded"
		job.FinishedAt = &now
		job.Result = result
		job.cancel = nil
		q.persistStatusLocked(job)
		q.appendLogLocked(id, jobEventSucceeded, result, "succeeded")
	}
}

func (q *serverJobQueue) execute(ctx context.Context, id string, request serverJobRequest) (map[string]any, error) {
	switch request.Type {
	case "chunks":
		return q.executeChunks(ctx, id, request)
	case "analysis":
		return q.executeAnalysis(ctx, id, request)
	default:
		return nil, fmt.Errorf("unsupported job type %q", request.Type)
	}
}

func (q *serverJobQueue) executeChunks(ctx context.Context, id string, request serverJobRequest) (map[string]any, error) {
	chunkSize := request.ChunkSize
	if chunkSize == 0 {
		chunkSize = 128
	}
	q.appendLog(id, jobEventChunksStarted, map[string]any{"chunk_size": chunkSize, "encoding": firstNonEmpty(request.Encoding, "json"), "force": request.Force}, "building chunks chunk_size=%d encoding=%s force=%t", chunkSize, firstNonEmpty(request.Encoding, "json"), request.Force)
	index, err := q.store.BuildFrameChunksWithOptions(ctx, request.DatasetID, mdsrv.BuildFrameChunksOptions{
		ChunkSize:      chunkSize,
		Encoding:       request.Encoding,
		GromacsCommand: q.options.GromacsCommand,
		Force:          request.Force,
		Limits:         q.options.Limits,
	})
	if err != nil {
		return nil, err
	}
	encodings := map[string]int{}
	for _, chunk := range index.Chunks {
		if chunk.Encoding != "" {
			encodings[chunk.Encoding]++
		}
	}
	q.appendLog(id, jobEventChunksCompleted, map[string]any{"frame_count": index.FrameCount, "chunk_count": len(index.Chunks), "atom_count": index.AtomCount}, "chunks complete frames=%d chunks=%d atoms=%d", index.FrameCount, len(index.Chunks), index.AtomCount)
	return map[string]any{
		"dataset_id":        index.DatasetID,
		"frame_count":       index.FrameCount,
		"atom_count":        index.AtomCount,
		"chunk_size_frames": index.ChunkSizeFrames,
		"chunk_count":       len(index.Chunks),
		"encodings":         encodings,
		"index":             filepath.ToSlash(filepath.Join(mdsrv.IndexesDir, request.DatasetID+"-frame-index.json")),
	}, nil
}

func (q *serverJobQueue) executeAnalysis(ctx context.Context, id string, request serverJobRequest) (map[string]any, error) {
	manifest, err := q.store.LoadDataset(request.DatasetID)
	if err != nil {
		return nil, err
	}
	q.appendLog(id, jobEventAnalysisStarted, map[string]any{"analysis_type": request.Analysis.Type, "backend": firstNonEmpty(request.Backend, q.options.Backend)}, "running analysis type=%s backend=%s", request.Analysis.Type, firstNonEmpty(request.Backend, q.options.Backend))
	trace, err := analyzeWithPolicy(ctx, q.store, manifest, request.DatasetID, request.Analysis, firstNonEmpty(request.Backend, q.options.Backend), q.options.GromacsCommand)
	if err != nil {
		return nil, err
	}
	output := request.Analysis.Output
	if output == "" {
		format := firstNonEmpty(request.Analysis.Format, "csv")
		output = filepath.ToSlash(filepath.Join("traces", request.DatasetID+"-"+firstNonEmpty(request.Analysis.ID, request.Analysis.Type)+"."+format))
	}
	// Confine the output to the store: an untrusted absolute path or "../"
	// escape must not let a job write files outside the store root.
	outputPath, err := q.store.SafeResolvePath(output)
	if err != nil {
		return nil, fmt.Errorf("analysis output %q escapes the store: %w", output, err)
	}
	if err := mdsrv.WriteTrace(outputPath, request.Analysis.Format, trace); err != nil {
		return nil, err
	}
	recordedOutput := output
	if relative, ok := storeRelativePathIfInside(q.store.Root, outputPath); ok {
		recordedOutput = relative
	}
	if err := q.store.RecordAnalysis(request.DatasetID, mdsrv.Analysis{
		ID:             firstNonEmpty(request.Analysis.ID, request.Analysis.Type),
		Type:           request.Analysis.Type,
		Selection:      request.Analysis.Selection,
		Selections:     request.Analysis.Selections,
		ReferenceFrame: request.Analysis.ReferenceFrame,
		Cutoff:         request.Analysis.Cutoff,
		Frames:         "all",
		Output:         recordedOutput,
	}); err != nil {
		return nil, err
	}
	q.appendLog(id, jobEventAnalysisCompleted, map[string]any{"values": len(trace.Values), "output": recordedOutput}, "analysis complete values=%d output=%s", len(trace.Values), recordedOutput)
	return map[string]any{
		"dataset_id":    request.DatasetID,
		"trace":         recordedOutput,
		"id":            trace.ID,
		"type":          trace.Type,
		"unit":          trace.Unit,
		"values":        len(trace.Values),
		"backend":       trace.Backend,
		"output_format": firstNonEmpty(request.Analysis.Format, strings.TrimPrefix(filepath.Ext(outputPath), "."), "csv"),
	}, nil
}

func (q *serverJobQueue) loadPersistedJobs() {
	jobsRoot := filepath.Join(q.store.Root, mdsrv.JobsDir)
	if err := os.MkdirAll(jobsRoot, 0o755); err != nil {
		return
	}
	entries, err := os.ReadDir(jobsRoot)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(jobsRoot, entry.Name(), "status.json"))
		if err != nil {
			continue
		}
		var status serverJobStatus
		if err := json.Unmarshal(raw, &status); err != nil || status.ID == "" {
			continue
		}
		if status.Status == "queued" || status.Status == "running" {
			status.Status = "failed"
			status.FinishedAt = &now
			status.Error = "job was interrupted before completion"
		}
		record := &serverJobRecord{serverJobStatus: status}
		q.jobs[status.ID] = record
		q.persistStatusLocked(record)
	}
}

func (q *serverJobQueue) jobDir(id string) string {
	return filepath.Join(q.store.Root, mdsrv.JobsDir, id)
}

func (q *serverJobQueue) statusPath(id string) string {
	return filepath.Join(q.jobDir(id), "status.json")
}

func (q *serverJobQueue) logPath(id string) string {
	return filepath.Join(q.jobDir(id), "job.log")
}

func (q *serverJobQueue) eventsPath(id string) string {
	return filepath.Join(q.jobDir(id), "events.jsonl")
}

func (q *serverJobQueue) persistStatusLocked(job *serverJobRecord) {
	if err := os.MkdirAll(q.jobDir(job.ID), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(job.serverJobStatus, "", "  ")
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	tmp := q.statusPath(job.ID) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, q.statusPath(job.ID))
}

func (q *serverJobQueue) appendLog(id string, eventType jobEventType, fields map[string]any, format string, args ...any) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.appendLogLocked(id, eventType, fields, format, args...)
}

func (q *serverJobQueue) appendLogLocked(id string, eventType jobEventType, fields map[string]any, format string, args ...any) {
	if err := os.MkdirAll(q.jobDir(id), 0o755); err != nil {
		return
	}
	line := fmt.Sprintf(format, args...)
	now := time.Now().UTC()
	line = fmt.Sprintf("%s %s\n", now.Format(time.RFC3339Nano), line)
	file, err := os.OpenFile(q.logPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(line)
	status := ""
	if job, ok := q.jobs[id]; ok {
		status = job.Status
	}
	q.appendEventLocked(serverJobEvent{
		At:      now,
		ID:      id,
		Status:  status,
		Type:    eventType,
		Message: fmt.Sprintf(format, args...),
		Fields:  fields,
	})
}

func (q *serverJobQueue) appendEventLocked(event serverJobEvent) {
	if event.Type == "" {
		event.Type = jobEventSubmitted
	}
	event.Version = jobEventVersion
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	file, err := os.OpenFile(q.eventsPath(event.ID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(raw, '\n'))
}

func (q *serverJobQueue) readLog(id string) (string, error) {
	raw, err := os.ReadFile(q.logPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (q *serverJobQueue) readEvents(id string) ([]serverJobEvent, []byte, error) {
	raw, err := os.ReadFile(q.eventsPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var events []serverJobEvent
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event serverJobEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, raw, err
		}
		if event.Version == "" {
			event.Version = jobEventVersion
		}
		events = append(events, event)
	}
	return events, raw, nil
}
