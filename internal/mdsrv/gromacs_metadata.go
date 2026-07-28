package mdsrv

import "github.com/rafflesia-ai/hlmdsrv/internal/gromacs"

func ProbeFromFileRef(ref FileRef) gromacs.TrajectoryProbe {
	return gromacs.TrajectoryProbe{
		AtomCount:  ref.AtomCount,
		FrameCount: ref.FrameCount,
		TimeStart:  ref.TimeStart,
		TimeEnd:    ref.TimeEnd,
		TimeStep:   ref.TimeStep,
	}
}

func ApplyProbe(ref *FileRef, probe gromacs.TrajectoryProbe) {
	ref.AtomCount = probe.AtomCount
	ref.FrameCount = probe.FrameCount
	ref.TimeStart = probe.TimeStart
	ref.TimeEnd = probe.TimeEnd
	ref.TimeStep = probe.TimeStep
}
