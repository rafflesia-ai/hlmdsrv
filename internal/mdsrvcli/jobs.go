package mdsrvcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type jobsFlags struct {
	server         string
	token          string
	jsonReport     bool
	request        string
	jobType        string
	datasetID      string
	backend        string
	chunkSize      int
	encoding       string
	force          bool
	analysisID     string
	analysisType   string
	selection      string
	a              string
	b              string
	c              string
	d              string
	output         string
	format         string
	timeoutSeconds int
	wait           bool
	interval       time.Duration
	waitTimeout    time.Duration
	store          string
	ttl            time.Duration
	statuses       []string
	dryRun         bool
}

type jobsClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func (a app) jobsCommand() *cobra.Command {
	flags := &jobsFlags{}
	cmd := &cobra.Command{Use: "jobs", Short: "Submit and inspect async server jobs"}
	cmd.PersistentFlags().StringVar(&flags.server, "server", "", "MDsrv server URL; defaults to MDSRV_SERVER_URL or http://127.0.0.1:1337")
	cmd.PersistentFlags().StringVar(&flags.token, "token", "", "bearer token or X-MDSRV-Token value; defaults to MDSRV_AUTH_TOKEN")
	cmd.PersistentFlags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")

	list := &cobra.Command{
		Use:   "list",
		Short: "List async jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJobsClient(flags)
			if err != nil {
				return err
			}
			var statuses []serverJobStatus
			if err := client.doJSON(cmd.Context(), http.MethodGet, "/jobs", nil, &statuses); err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, statuses)
			}
			for _, status := range statuses {
				fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\n", status.ID, status.Status, status.Type, status.DatasetID)
			}
			return nil
		},
	}

	submit := &cobra.Command{
		Use:   "submit",
		Short: "Submit an async chunking or analysis job",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJobsClient(flags)
			if err != nil {
				return err
			}
			request, err := buildJobRequest(flags)
			if err != nil {
				return err
			}
			var status serverJobStatus
			if err := client.doJSON(cmd.Context(), http.MethodPost, "/jobs", request, &status); err != nil {
				return err
			}
			if flags.wait {
				status, err = waitForJob(cmd.Context(), client, status.ID, flags.interval, flags.waitTimeout)
				if err != nil {
					return err
				}
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, status)
			}
			fmt.Fprintf(a.stdout, "%s\t%s\n", status.ID, status.Status)
			return nil
		},
	}
	submit.Flags().StringVar(&flags.request, "request", "", "JSON file containing the full job request")
	submit.Flags().StringVar(&flags.jobType, "type", "", "job type: chunks or analysis")
	submit.Flags().StringVar(&flags.datasetID, "dataset", "", "dataset id")
	submit.Flags().StringVar(&flags.backend, "backend", "", "analysis backend override")
	submit.Flags().IntVar(&flags.chunkSize, "chunk-size", 0, "chunk size in frames for chunks jobs")
	submit.Flags().StringVar(&flags.encoding, "encoding", "", "chunk encoding: json, bin, or bin-zstd")
	submit.Flags().BoolVar(&flags.force, "force", false, "overwrite existing output")
	submit.Flags().StringVar(&flags.analysisID, "analysis-id", "", "analysis id")
	submit.Flags().StringVar(&flags.analysisType, "analysis-type", "", "analysis type: distance, angle, dihedral, rmsd, rmsf, radius-of-gyration, contacts")
	submit.Flags().StringVar(&flags.selection, "selection", "", "single analysis selection")
	submit.Flags().StringVar(&flags.a, "a", "", "analysis selection a")
	submit.Flags().StringVar(&flags.b, "b", "", "analysis selection b")
	submit.Flags().StringVar(&flags.c, "c", "", "analysis selection c")
	submit.Flags().StringVar(&flags.d, "d", "", "analysis selection d")
	submit.Flags().StringVar(&flags.output, "out", "", "analysis output path")
	submit.Flags().StringVar(&flags.format, "format", "", "analysis output format")
	submit.Flags().IntVar(&flags.timeoutSeconds, "timeout-seconds", 0, "per-job timeout in seconds")
	submit.Flags().BoolVar(&flags.wait, "wait", false, "wait until the job reaches a terminal status")
	submit.Flags().DurationVar(&flags.interval, "interval", 500*time.Millisecond, "poll interval for --wait")
	submit.Flags().DurationVar(&flags.waitTimeout, "wait-timeout", 0, "maximum time to wait; 0 means use command context")

	status := &cobra.Command{
		Use:   "status JOB_ID",
		Short: "Show job status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJobsClient(flags)
			if err != nil {
				return err
			}
			var status serverJobStatus
			if err := client.doJSON(cmd.Context(), http.MethodGet, "/jobs/"+url.PathEscape(args[0]), nil, &status); err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, status)
			}
			fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\n", status.ID, status.Status, status.Type, status.DatasetID)
			return nil
		},
	}

	wait := &cobra.Command{
		Use:   "wait JOB_ID",
		Short: "Wait for a job to finish",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJobsClient(flags)
			if err != nil {
				return err
			}
			status, err := waitForJob(cmd.Context(), client, args[0], flags.interval, flags.waitTimeout)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, status)
			}
			fmt.Fprintf(a.stdout, "%s\t%s\n", status.ID, status.Status)
			return nil
		},
	}
	wait.Flags().DurationVar(&flags.interval, "interval", 500*time.Millisecond, "poll interval")
	wait.Flags().DurationVar(&flags.waitTimeout, "wait-timeout", 0, "maximum time to wait; 0 means use command context")

	logs := &cobra.Command{
		Use:   "logs JOB_ID",
		Short: "Print job logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJobsClient(flags)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				var log serverJobLog
				if err := client.doJSON(cmd.Context(), http.MethodGet, "/jobs/"+url.PathEscape(args[0])+"/logs?format=json", nil, &log); err != nil {
					return err
				}
				return writeJSON(a.stdout, log)
			}
			log, err := client.getText(cmd.Context(), "/jobs/"+url.PathEscape(args[0])+"/logs")
			if err != nil {
				return err
			}
			fmt.Fprint(a.stdout, log)
			return nil
		},
	}

	events := &cobra.Command{
		Use:   "events JOB_ID",
		Short: "Print structured job events as JSON Lines",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJobsClient(flags)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				var payload struct {
					ID     string           `json:"id"`
					Events []serverJobEvent `json:"events"`
				}
				if err := client.doJSON(cmd.Context(), http.MethodGet, "/jobs/"+url.PathEscape(args[0])+"/events?format=json", nil, &payload); err != nil {
					return err
				}
				return writeJSON(a.stdout, payload)
			}
			events, err := client.getText(cmd.Context(), "/jobs/"+url.PathEscape(args[0])+"/events")
			if err != nil {
				return err
			}
			fmt.Fprint(a.stdout, events)
			return nil
		},
	}

	cancel := &cobra.Command{
		Use:   "cancel JOB_ID",
		Short: "Cancel a queued or running job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJobsClient(flags)
			if err != nil {
				return err
			}
			var status serverJobStatus
			if err := client.doJSON(cmd.Context(), http.MethodPost, "/jobs/"+url.PathEscape(args[0])+"/cancel", map[string]any{}, &status); err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, status)
			}
			fmt.Fprintf(a.stdout, "%s\t%s\n", status.ID, status.Status)
			return nil
		},
	}

	retry := &cobra.Command{
		Use:   "retry JOB_ID",
		Short: "Retry a terminal job using its original request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJobsClient(flags)
			if err != nil {
				return err
			}
			var status serverJobStatus
			if err := client.doJSON(cmd.Context(), http.MethodPost, "/jobs/"+url.PathEscape(args[0])+"/retry", map[string]any{}, &status); err != nil {
				return err
			}
			if flags.wait {
				status, err = waitForJob(cmd.Context(), client, status.ID, flags.interval, flags.waitTimeout)
				if err != nil {
					return err
				}
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, status)
			}
			fmt.Fprintf(a.stdout, "%s\t%s\n", status.ID, status.Status)
			return nil
		},
	}
	retry.Flags().BoolVar(&flags.wait, "wait", false, "wait until the retried job reaches a terminal status")
	retry.Flags().DurationVar(&flags.interval, "interval", 500*time.Millisecond, "poll interval for --wait")
	retry.Flags().DurationVar(&flags.waitTimeout, "wait-timeout", 0, "maximum time to wait; 0 means use command context")

	stats := &cobra.Command{
		Use:   "stats",
		Short: "Show server job queue statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJobsClient(flags)
			if err != nil {
				return err
			}
			var stats serverJobStats
			if err := client.doJSON(cmd.Context(), http.MethodGet, "/jobs/stats", nil, &stats); err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, stats)
			}
			keys := make([]string, 0, len(stats.Counts))
			for key := range stats.Counts {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			fmt.Fprintf(a.stdout, "workers\t%d\nmax_queue\t%d\nqueue_depth\t%d\ntotal\t%d\n", stats.Workers, stats.MaxQueue, stats.QueuedChannelDepth, stats.Total)
			for _, key := range keys {
				fmt.Fprintf(a.stdout, "%s\t%d\n", key, stats.Counts[key])
			}
			if stats.OldestQueuedAt != nil {
				fmt.Fprintf(a.stdout, "oldest_queued_age_seconds\t%.3f\n", stats.OldestQueuedAgeSeconds)
			}
			return nil
		},
	}

	prune := &cobra.Command{
		Use:   "prune",
		Short: "Prune persisted local job records",
		RunE: func(cmd *cobra.Command, args []string) error {
			statusFilter := map[string]bool{}
			for _, status := range flags.statuses {
				status = strings.ToLower(strings.TrimSpace(status))
				if status == "" {
					continue
				}
				if !isKnownJobStatus(status) {
					return fmt.Errorf("unsupported status %q", status)
				}
				statusFilter[status] = true
			}
			report, err := pruneJobs(jobPruneOptions{
				Store:  flags.store,
				TTL:    flags.ttl,
				Status: statusFilter,
				DryRun: flags.dryRun,
			})
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			for _, id := range report.WouldRemove {
				fmt.Fprintf(a.stdout, "would_remove\t%s\n", id)
			}
			for _, id := range report.Removed {
				fmt.Fprintf(a.stdout, "removed\t%s\n", id)
			}
			for _, item := range report.Errors {
				fmt.Fprintf(a.stderr, "error\t%s\n", item)
			}
			return nil
		},
	}
	prune.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	prune.Flags().DurationVar(&flags.ttl, "ttl", 24*time.Hour, "remove jobs older than this duration; 0 removes all matching statuses")
	prune.Flags().StringSliceVar(&flags.statuses, "status", []string{"succeeded", "failed", "canceled"}, "job statuses to prune; repeat or comma-separate")
	prune.Flags().BoolVar(&flags.dryRun, "dry-run", false, "report matching jobs without deleting them")

	cmd.AddCommand(list, submit, status, wait, logs, events, cancel, retry, stats, prune)
	return cmd
}

func newJobsClient(flags *jobsFlags) (jobsClient, error) {
	base := strings.TrimRight(firstNonEmpty(flags.server, os.Getenv("MDSRV_SERVER_URL"), "http://127.0.0.1:1337"), "/")
	if _, err := url.ParseRequestURI(base); err != nil {
		return jobsClient{}, fmt.Errorf("invalid server URL %q: %w", base, err)
	}
	return jobsClient{
		baseURL: base,
		token:   firstNonEmpty(flags.token, os.Getenv("MDSRV_AUTH_TOKEN")),
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c jobsClient) doJSON(ctx context.Context, method string, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
		request.Header.Set("X-MDSRV-Token", c.token)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("server returned %s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c jobsClient) getText(ctx context.Context, path string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
		request.Header.Set("X-MDSRV-Token", c.token)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("server returned %s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	return string(raw), nil
}

func buildJobRequest(flags *jobsFlags) (serverJobRequest, error) {
	if flags.request != "" {
		raw, err := os.ReadFile(flags.request)
		if err != nil {
			return serverJobRequest{}, err
		}
		var request serverJobRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			return serverJobRequest{}, err
		}
		return request, nil
	}
	request := serverJobRequest{
		Type:           flags.jobType,
		DatasetID:      flags.datasetID,
		Backend:        flags.backend,
		ChunkSize:      flags.chunkSize,
		Encoding:       flags.encoding,
		Force:          flags.force,
		TimeoutSeconds: flags.timeoutSeconds,
	}
	if strings.TrimSpace(request.Type) == "analysis" || flags.analysisType != "" {
		request.Type = "analysis"
		request.Analysis = mdsrv.AnalysisRequest{
			ID:        flags.analysisID,
			Type:      flags.analysisType,
			Selection: flags.selection,
			Selections: map[string]string{
				"a": flags.a,
				"b": flags.b,
				"c": flags.c,
				"d": flags.d,
			},
			Output: flags.output,
			Format: flags.format,
		}
		for key, value := range request.Analysis.Selections {
			if strings.TrimSpace(value) == "" {
				delete(request.Analysis.Selections, key)
			}
		}
	}
	if strings.TrimSpace(request.Type) == "" {
		return serverJobRequest{}, errorsNew("job --type is required unless --request is provided")
	}
	if strings.TrimSpace(request.DatasetID) == "" {
		return serverJobRequest{}, errorsNew("job --dataset is required unless --request is provided")
	}
	return request, nil
}

func waitForJob(ctx context.Context, client jobsClient, id string, interval time.Duration, timeout time.Duration) (serverJobStatus, error) {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		var status serverJobStatus
		if err := client.doJSON(ctx, http.MethodGet, "/jobs/"+url.PathEscape(id), nil, &status); err != nil {
			return serverJobStatus{}, err
		}
		if isTerminalJobStatus(status.Status) {
			return status, nil
		}
		select {
		case <-ctx.Done():
			return serverJobStatus{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func isTerminalJobStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "canceled":
		return true
	default:
		return false
	}
}

func isKnownJobStatus(status string) bool {
	switch status {
	case "queued", "running", "succeeded", "failed", "canceled":
		return true
	default:
		return false
	}
}

func errorsNew(message string) error {
	return fmt.Errorf("%s", message)
}
