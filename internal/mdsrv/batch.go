package mdsrv

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type BatchJob struct {
	ID            string `json:"id" yaml:"id"`
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
}

func (j BatchJob) IngestOptions(force bool) IngestOptions {
	return IngestOptions{
		ID:            j.ID,
		Name:          j.Name,
		Description:   j.Description,
		Source:        j.Source,
		License:       j.License,
		CreatedBy:     j.CreatedBy,
		Topology:      j.Topology,
		TopologyURL:   j.TopologyURL,
		Trajectory:    j.Trajectory,
		TrajectoryURL: j.TrajectoryURL,
		Cache:         j.Cache,
		Stride:        j.Stride,
		AtomSubset:    j.AtomSubset,
		TimeUnit:      j.TimeUnit,
		CoordUnit:     j.CoordUnit,
		Force:         force,
	}
}

func LoadBatchFile(path string) ([]BatchJob, error) {
	if strings.EqualFold(filepath.Ext(path), ".jsonl") {
		return loadBatchJSONL(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var jobs []BatchJob
	if isJSONPath(path) || json.Valid(data) {
		if err := json.Unmarshal(data, &jobs); err == nil && len(jobs) > 0 {
			return jobs, nil
		}
		var job BatchJob
		if err := json.Unmarshal(data, &job); err != nil {
			return nil, fmt.Errorf("decode json batch: %w", err)
		}
		return []BatchJob{job}, nil
	}
	if err := yaml.Unmarshal(data, &jobs); err == nil && len(jobs) > 0 {
		return jobs, nil
	}
	var job BatchJob
	if err := yaml.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("decode yaml batch: %w", err)
	}
	return []BatchJob{job}, nil
}

func loadBatchJSONL(path string) ([]BatchJob, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var jobs []BatchJob
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var job BatchJob
		if err := json.Unmarshal(raw, &job); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		jobs = append(jobs, job)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("%s does not contain any batch jobs", path)
	}
	return jobs, nil
}
