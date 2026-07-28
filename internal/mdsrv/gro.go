package mdsrv

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// maxGROAtomPrealloc caps how many coordinate slots are reserved up front from
// a .gro file's declared atom count. Real systems can exceed this; append grows
// the slice as genuine lines are read. It only bounds the allocation a crafted
// or corrupt count can force before any coordinate line is parsed.
const maxGROAtomPrealloc = 1 << 20

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func ParseGROFrame(path string, frameIndex int) (Frame, error) {
	file, err := os.Open(path)
	if err != nil {
		return Frame{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return Frame{}, fmt.Errorf("%s is empty", path)
	}
	title := scanner.Text()
	timeValue := parseGROTime(title)
	if !scanner.Scan() {
		return Frame{}, fmt.Errorf("%s is missing atom count", path)
	}
	atomCount, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil {
		return Frame{}, fmt.Errorf("parse gro atom count: %w", err)
	}
	if atomCount < 0 {
		return Frame{}, fmt.Errorf("parse gro atom count: negative count %d", atomCount)
	}
	// Cap the preallocation so a crafted atom count cannot force a huge
	// up-front allocation (a negative count would also panic make). The loop
	// still reads exactly atomCount lines via append and errors early when the
	// file is shorter than the declared count.
	coords := make([][3]float32, 0, minInt(atomCount, maxGROAtomPrealloc))
	for i := 0; i < atomCount; i++ {
		if !scanner.Scan() {
			return Frame{}, fmt.Errorf("%s ended before atom %d", path, i+1)
		}
		coord, err := parseGROCoord(scanner.Text())
		if err != nil {
			return Frame{}, fmt.Errorf("atom %d: %w", i+1, err)
		}
		coords = append(coords, coord)
	}
	var unitCell [][3]float32
	if scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 {
			x, _ := strconv.ParseFloat(fields[0], 32)
			y, _ := strconv.ParseFloat(fields[1], 32)
			z, _ := strconv.ParseFloat(fields[2], 32)
			unitCell = [][3]float32{
				{float32(x), 0, 0},
				{0, float32(y), 0},
				{0, 0, float32(z)},
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Frame{}, err
	}
	return Frame{
		Backend:        "gromacs",
		Frame:          frameIndex,
		Time:           timeValue,
		TimeUnit:       "ps",
		CoordinateUnit: "nm",
		UnitCell:       unitCell,
		Coordinates:    coords,
	}, nil
}

func parseGROCoord(line string) ([3]float32, error) {
	fields := strings.Fields(line)
	// GRO position and velocity values are fixed-point (e.g. %8.3f / %8.4f) and
	// therefore always contain a decimal point, whereas the residue id, residue
	// name, atom name, and atom number never do. The trailing decimal fields are
	// the coordinate block: 3 values (positions only) or 6 (positions followed
	// by velocities). Positions are always the FIRST three of that block —
	// taking the last three returns the velocities for trajectories that carry
	// them, placing every atom at the wrong location.
	coords := make([]string, 0, 6)
	for _, f := range fields {
		if strings.ContainsRune(f, '.') {
			coords = append(coords, f)
		}
	}
	if len(coords) < 3 {
		// Fall back to the trailing three fields for atypical lines whose
		// coordinates lack a decimal point (e.g. integer-formatted values).
		if len(fields) < 3 {
			return [3]float32{}, fmt.Errorf("invalid coordinate line %q", line)
		}
		coords = fields[len(fields)-3:]
	}
	x, err := strconv.ParseFloat(coords[0], 32)
	if err != nil {
		return [3]float32{}, err
	}
	y, err := strconv.ParseFloat(coords[1], 32)
	if err != nil {
		return [3]float32{}, err
	}
	z, err := strconv.ParseFloat(coords[2], 32)
	if err != nil {
		return [3]float32{}, err
	}
	return [3]float32{float32(x), float32(y), float32(z)}, nil
}

func parseGROTime(title string) float64 {
	index := strings.LastIndex(title, "t=")
	if index < 0 {
		return 0
	}
	value := strings.TrimSpace(title[index+2:])
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	parsed, _ := strconv.ParseFloat(fields[0], 64)
	return parsed
}
