package mdsrvcli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type benchFlags struct {
	frames     int
	atoms      int
	iterations int
	out        string
	jsonReport bool
}

type benchReport struct {
	OK         bool               `json:"ok"`
	StartedAt  time.Time          `json:"started_at"`
	Frames     int                `json:"frames"`
	Atoms      int                `json:"atoms"`
	Iterations int                `json:"iterations"`
	Results    []benchCodecResult `json:"results"`
}

type benchCodecResult struct {
	Encoding        string  `json:"encoding"`
	Bytes           int     `json:"bytes"`
	EncodeAverageMS float64 `json:"encode_average_ms"`
	DecodeAverageMS float64 `json:"decode_average_ms"`
	FramesPerSecond float64 `json:"frames_per_second"`
}

func (a app) benchCommand() *cobra.Command {
	flags := &benchFlags{}
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Run synthetic MDsrv frame chunk benchmarks",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := runBench(flags)
			if err != nil {
				return err
			}
			if flags.out != "" {
				if err := writeJSONFile(flags.out, report); err != nil {
					return err
				}
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			for _, result := range report.Results {
				fmt.Fprintf(a.stdout, "%s\tbytes=%d\tencode=%.3fms\tdecode=%.3fms\tfps=%.1f\n", result.Encoding, result.Bytes, result.EncodeAverageMS, result.DecodeAverageMS, result.FramesPerSecond)
			}
			if flags.out != "" {
				fmt.Fprintf(a.stdout, "report\t%s\n", flags.out)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&flags.frames, "frames", 128, "synthetic frame count")
	cmd.Flags().IntVar(&flags.atoms, "atoms", 1024, "synthetic atom count")
	cmd.Flags().IntVar(&flags.iterations, "iterations", 3, "iterations per codec")
	cmd.Flags().StringVar(&flags.out, "out", "", "write benchmark report JSON to this path")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func runBench(flags *benchFlags) (benchReport, error) {
	if flags.frames < 1 {
		return benchReport{}, fmt.Errorf("--frames must be at least 1")
	}
	if flags.atoms < 1 {
		return benchReport{}, fmt.Errorf("--atoms must be at least 1")
	}
	if flags.iterations < 1 {
		return benchReport{}, fmt.Errorf("--iterations must be at least 1")
	}
	chunk := syntheticBenchChunk(flags.frames, flags.atoms)
	report := benchReport{
		OK:         true,
		StartedAt:  time.Now().UTC(),
		Frames:     flags.frames,
		Atoms:      flags.atoms,
		Iterations: flags.iterations,
	}
	for _, encoding := range []string{"json", "bin", "bin-zstd"} {
		chunk.Encoding = encoding
		var encoded []byte
		var normalized string
		var encodeElapsed time.Duration
		var decodeElapsed time.Duration
		for i := 0; i < flags.iterations; i++ {
			start := time.Now()
			raw, codec, err := mdsrv.EncodeFrameChunk(chunk)
			if err != nil {
				return benchReport{}, err
			}
			encodeElapsed += time.Since(start)
			encoded = raw
			normalized = codec
			start = time.Now()
			if _, err := mdsrv.DecodeFrameChunk(raw, codec); err != nil {
				return benchReport{}, err
			}
			decodeElapsed += time.Since(start)
		}
		totalSeconds := (encodeElapsed + decodeElapsed).Seconds()
		fps := 0.0
		if totalSeconds > 0 {
			fps = float64(flags.frames*flags.iterations) / totalSeconds
		}
		report.Results = append(report.Results, benchCodecResult{
			Encoding:        normalized,
			Bytes:           len(encoded),
			EncodeAverageMS: float64(encodeElapsed.Microseconds()) / 1000 / float64(flags.iterations),
			DecodeAverageMS: float64(decodeElapsed.Microseconds()) / 1000 / float64(flags.iterations),
			FramesPerSecond: fps,
		})
	}
	return report, nil
}

func syntheticBenchChunk(frames int, atoms int) mdsrv.FrameChunkData {
	data := mdsrv.FrameChunkData{
		DatasetID: "bench",
		Chunk:     0,
		Start:     0,
		Stop:      frames,
		Encoding:  "json",
		Frames:    make([]mdsrv.Frame, frames),
	}
	for frameIndex := 0; frameIndex < frames; frameIndex++ {
		frame := mdsrv.Frame{
			Backend:        "synthetic",
			Frame:          frameIndex,
			Time:           float64(frameIndex),
			TimeUnit:       "ps",
			CoordinateUnit: "nm",
			Coordinates:    make([][3]float32, atoms),
		}
		for atomIndex := 0; atomIndex < atoms; atomIndex++ {
			frame.Coordinates[atomIndex] = [3]float32{
				float32(frameIndex) * 0.01,
				float32(atomIndex) * 0.001,
				float32((frameIndex + atomIndex) % 17),
			}
		}
		data.Frames[frameIndex] = frame
	}
	return data
}
