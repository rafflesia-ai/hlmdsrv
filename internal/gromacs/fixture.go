package gromacs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DemoTrajectoryOptions struct {
	OutDir  string
	Frames  int
	Force   bool
	Command string
}

type DemoTrajectory struct {
	Topology   string `json:"topology"`
	Frames     string `json:"frames"`
	Trajectory string `json:"trajectory"`
	AtomCount  int    `json:"atom_count"`
	FrameCount int    `json:"frame_count"`
}

func CreateDemoTrajectory(ctx context.Context, opts DemoTrajectoryOptions) (DemoTrajectory, error) {
	if opts.Frames < 1 {
		return DemoTrajectory{}, errors.New("frames must be at least 1")
	}
	outDir := strings.TrimSpace(opts.OutDir)
	if outDir == "" {
		outDir = "."
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return DemoTrajectory{}, err
	}
	demo := DemoTrajectory{
		Topology:   filepath.Join(outDir, "structure.gro"),
		Frames:     filepath.Join(outDir, "frames.gro"),
		Trajectory: filepath.Join(outDir, "trajectory.xtc"),
		AtomCount:  3,
		FrameCount: opts.Frames,
	}
	if !opts.Force {
		for _, path := range []string{demo.Topology, demo.Frames, demo.Trajectory} {
			if _, err := os.Stat(path); err == nil {
				return DemoTrajectory{}, fmt.Errorf("%s already exists; pass --force to overwrite", path)
			} else if err != nil && !os.IsNotExist(err) {
				return DemoTrajectory{}, err
			}
		}
	}
	gmx := New(Options{Command: opts.Command})
	if err := gmx.RequireAvailable(); err != nil {
		return DemoTrajectory{}, err
	}
	if err := os.WriteFile(demo.Topology, []byte(DemoGRO(1)), 0o644); err != nil {
		return DemoTrajectory{}, err
	}
	if err := os.WriteFile(demo.Frames, []byte(DemoGRO(opts.Frames)), 0o644); err != nil {
		return DemoTrajectory{}, err
	}
	if err := gmx.Convert(ctx, ConvertOptions{Input: demo.Frames, Output: demo.Trajectory}); err != nil {
		return DemoTrajectory{}, err
	}
	return demo, nil
}

func DemoGRO(frames int) string {
	var b strings.Builder
	for frame := 0; frame < frames; frame++ {
		t := float64(frame)
		shift := 0.01 * float64(frame)
		fmt.Fprintf(&b, "Demo t= %.1f\n", t)
		fmt.Fprintf(&b, "%5d\n", 3)
		fmt.Fprintf(&b, "%5d%-5s%5s%5d%8.3f%8.3f%8.3f\n", 1, "MOL", "C1", 1, 0.100+shift, 0.100, 0.100)
		fmt.Fprintf(&b, "%5d%-5s%5s%5d%8.3f%8.3f%8.3f\n", 1, "MOL", "O1", 2, 0.200+shift, 0.100+shift, 0.100)
		fmt.Fprintf(&b, "%5d%-5s%5s%5d%8.3f%8.3f%8.3f\n", 1, "MOL", "H1", 3, 0.300+shift, 0.100, 0.100+shift)
		fmt.Fprintln(&b, "   1.00000   1.00000   1.00000")
	}
	return b.String()
}
