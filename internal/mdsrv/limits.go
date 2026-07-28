package mdsrv

import (
	"fmt"

	"github.com/rafflesia-ai/hlmdsrv/internal/gromacs"
)

type ResourceLimits struct {
	MaxAtoms      int   `json:"max_atoms,omitempty" yaml:"max_atoms,omitempty"`
	MaxFrames     int   `json:"max_frames,omitempty" yaml:"max_frames,omitempty"`
	MaxChunkBytes int64 `json:"max_chunk_bytes,omitempty" yaml:"max_chunk_bytes,omitempty"`
}

func (limits ResourceLimits) Validate() error {
	if limits.MaxAtoms < 0 {
		return fmt.Errorf("max_atoms cannot be negative")
	}
	if limits.MaxFrames < 0 {
		return fmt.Errorf("max_frames cannot be negative")
	}
	if limits.MaxChunkBytes < 0 {
		return fmt.Errorf("max_chunk_bytes cannot be negative")
	}
	return nil
}

func (limits ResourceLimits) CheckProbe(label string, probe gromacs.TrajectoryProbe) error {
	if limits.MaxAtoms > 0 && probe.AtomCount > limits.MaxAtoms {
		return fmt.Errorf("%s has %d atoms, exceeding max_atoms=%d", label, probe.AtomCount, limits.MaxAtoms)
	}
	if limits.MaxFrames > 0 && probe.FrameCount > limits.MaxFrames {
		return fmt.Errorf("%s has %d frames, exceeding max_frames=%d", label, probe.FrameCount, limits.MaxFrames)
	}
	return nil
}

func (limits ResourceLimits) CheckManifest(m Manifest) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	if len(m.Inputs.Trajectories) == 0 {
		return nil
	}
	return limits.CheckProbe("trajectory", ProbeFromFileRef(m.Inputs.Trajectories[0]))
}

func (limits ResourceLimits) CheckFrame(label string, frame Frame) error {
	if limits.MaxAtoms > 0 && len(frame.Coordinates) > limits.MaxAtoms {
		return fmt.Errorf("%s has %d atoms, exceeding max_atoms=%d", label, len(frame.Coordinates), limits.MaxAtoms)
	}
	return nil
}

func (limits ResourceLimits) CheckChunkBytes(path string, size int) error {
	if limits.MaxChunkBytes > 0 && int64(size) > limits.MaxChunkBytes {
		return fmt.Errorf("%s is %d bytes, exceeding max_chunk_bytes=%d", path, size, limits.MaxChunkBytes)
	}
	return nil
}
