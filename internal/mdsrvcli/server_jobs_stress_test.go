package mdsrvcli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestJobQueueConcurrencyRace hammers the async job queue with concurrent
// submit / list / stats / status / cancel / retry while workers process jobs,
// so the mutex-guarded state machine and the panic-recovering worker run under
// contention. Its real assertion is the race detector (run with -race). Heavy,
// so skipped under -short. Jobs fail fast on the missing-gmx backend, which
// exercises the markFailed / executeGuarded paths.
func TestJobQueueConcurrencyRace(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test; skipped under -short")
	}
	store, _, _ := makeHTTPFixtureStore(t)
	mux := http.NewServeMux()
	registerHandlersWithOptions(mux, store, serverOptions{
		Backend:        "gromacs",
		GromacsCommand: "missing-gmx-for-stress-test",
		MaxFrameRange:  256,
		Workers:        4,
		MaxQueue:       256,
		JobTimeout:     2 * time.Second,
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := server.Client()

	get := func(path string) {
		resp, err := client.Get(server.URL + path)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}
	post := func(path, body string) (int, string) {
		resp, err := client.Post(server.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			return 0, ""
		}
		defer resp.Body.Close()
		var decoded struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&decoded)
		return resp.StatusCode, decoded.ID
	}

	var idMu sync.Mutex
	ids := make([]string, 0, 512)
	addID := func(id string) {
		if id == "" {
			return
		}
		idMu.Lock()
		ids = append(ids, id)
		idMu.Unlock()
	}
	sampleID := func(i int) string {
		idMu.Lock()
		defer idMu.Unlock()
		if len(ids) == 0 {
			return ""
		}
		return ids[i%len(ids)]
	}

	const submissions = 400
	var submitted int64

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for r := 0; r < 6; r++ {
		readers.Add(1)
		go func(seed int) {
			defer readers.Done()
			i := seed
			for {
				select {
				case <-stop:
					return
				default:
					get("/jobs")
					get("/jobs/stats")
					get("/jobs/metrics")
					if id := sampleID(i); id != "" {
						get("/jobs/" + id)
						if i%3 == 0 {
							_, _ = post("/jobs/"+id+"/cancel", "{}")
						}
						if i%5 == 0 {
							if _, rid := post("/jobs/"+id+"/retry", "{}"); rid != "" {
								addID(rid)
							}
						}
					}
					i++
				}
			}
		}(r)
	}

	var submitters sync.WaitGroup
	sem := make(chan struct{}, 24)
	for i := 0; i < submissions; i++ {
		submitters.Add(1)
		sem <- struct{}{}
		go func() {
			defer submitters.Done()
			defer func() { <-sem }()
			for attempt := 0; attempt < 500; attempt++ {
				status, id := post("/jobs", `{"type":"chunks","dataset_id":"run1","chunk_size":4,"encoding":"json"}`)
				if status == http.StatusTooManyRequests {
					time.Sleep(time.Millisecond)
					continue
				}
				addID(id)
				atomic.AddInt64(&submitted, 1)
				return
			}
		}()
	}
	submitters.Wait()

	// Drain: stop clients, then wait until no job is queued or running so no
	// worker goroutine is mid-flight when the test's temp store is cleaned up.
	close(stop)
	readers.Wait()
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := client.Get(server.URL + "/jobs/stats")
		if err != nil {
			t.Fatal(err)
		}
		var stats serverJobStats
		_ = json.NewDecoder(resp.Body).Decode(&stats)
		_ = resp.Body.Close()
		if stats.Counts["queued"] == 0 && stats.Counts["running"] == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("jobs did not drain: %#v", stats.Counts)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if submitted == 0 {
		t.Fatal("no jobs were submitted")
	}
}
