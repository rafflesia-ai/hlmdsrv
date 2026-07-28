package mdsrv

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func ParseAtomSelection(value string, atomCount int) ([]int, error) {
	value = normalizeAtomSelection(value)
	if value == "" {
		return nil, errors.New("atom index selection is required")
	}
	if value == "all" {
		result := make([]int, atomCount)
		for i := range result {
			result[i] = i
		}
		return result, nil
	}
	seen := map[int]bool{}
	var result []int
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, invalidAtomSelection(part)
			}
			end, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, invalidAtomSelection(part)
			}
			if end < start {
				return nil, fmt.Errorf("invalid descending range %d-%d", start, end)
			}
			for i := start; i <= end; i++ {
				if i < 1 || i > atomCount {
					return nil, fmt.Errorf("atom index %d out of range 1..%d", i, atomCount)
				}
				if !seen[i-1] {
					result = append(result, i-1)
					seen[i-1] = true
				}
			}
			continue
		}
		index, err := strconv.Atoi(part)
		if err != nil {
			return nil, invalidAtomSelection(part)
		}
		if index < 1 || index > atomCount {
			return nil, fmt.Errorf("atom index %d out of range 1..%d", index, atomCount)
		}
		if !seen[index-1] {
			result = append(result, index-1)
			seen[index-1] = true
		}
	}
	if len(result) == 0 {
		return nil, errors.New("selection matched no atoms")
	}
	return result, nil
}

func normalizeAtomSelection(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	for _, prefix := range []string{"atom:", "atoms:", "index:", "indices:", "atom ", "atoms ", "index ", "indices "} {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(value, prefix))
		}
	}
	return value
}

func AtomSelectionToIndexFile(name, expression string, atomCount int) (string, error) {
	atoms, err := ParseAtomSelection(expression, atomCount)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[ %s ]\n", name)
	for i, atom := range atoms {
		if i > 0 && i%15 == 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%d ", atom+1)
	}
	b.WriteByte('\n')
	return b.String(), nil
}

func FindSelection(m Manifest, value string) (Selection, bool) {
	key := strings.TrimSpace(value)
	key = strings.TrimPrefix(key, "@")
	if key == "" {
		return Selection{}, false
	}
	for _, selection := range m.Selections {
		if selection.ID == key {
			return selection, true
		}
	}
	return Selection{}, false
}

func ResolveSelectionExpression(m Manifest, value string) string {
	if selection, ok := FindSelection(m, value); ok {
		return selection.Expression
	}
	return value
}

func ResolveSelectionMap(m Manifest, values map[string]string) map[string]string {
	if len(values) == 0 {
		return values
	}
	resolved := make(map[string]string, len(values))
	for key, value := range values {
		resolved[key] = ResolveSelectionExpression(m, value)
	}
	return resolved
}

func ResolveSelectionForTarget(m Manifest, value string, target string, atomCount int) (string, error) {
	selection, ok := FindSelection(m, value)
	if !ok {
		return value, nil
	}
	kind := normalizeSelectionKind(selection.Kind)
	if kind == "" {
		kind = "atom-index"
	}
	target = normalizeSelectionKind(target)
	switch {
	case target == "" || target == "raw":
		return selection.Expression, nil
	case kind == target:
		return selection.Expression, nil
	case kind == "atom-index":
		return convertAtomIndexSelection(selection.Expression, target, atomCount)
	case kind == "mdtraj" && target == "python":
		return selection.Expression, nil
	case kind == "mdanalysis" && target == "python":
		return selection.Expression, nil
	case kind == "molstar" && target == "mvs":
		return selection.Expression, nil
	default:
		return "", fmt.Errorf("selection %q has kind %q and cannot be used as %q", selection.ID, kind, target)
	}
}

func ResolveSelectionMapForTarget(m Manifest, values map[string]string, target string, atomCount int) (map[string]string, error) {
	if len(values) == 0 {
		return values, nil
	}
	resolved := make(map[string]string, len(values))
	for key, value := range values {
		next, err := ResolveSelectionForTarget(m, value, target, atomCount)
		if err != nil {
			return nil, fmt.Errorf("selection %s: %w", key, err)
		}
		resolved[key] = next
	}
	return resolved, nil
}

// invalidAtomSelection reports an unparseable atom-index term without leaking Go
// internals: a malformed expression used to surface the raw
// `strconv.Atoi: parsing "!!!": invalid syntax`, which names an implementation
// detail rather than the thing the caller typed.
func invalidAtomSelection(part string) error {
	return fmt.Errorf("invalid atom index selection %q: expected an index or START-END range", part)
}

// SelectionKinds are the selection dialects the store understands. "" and "raw"
// mean an uninterpreted passthrough expression.
var SelectionKinds = []string{"atom-index", "mdtraj", "mdanalysis", "python", "mvs", "raw"}

// ValidateSelectionKind rejects a kind outside the known set. normalizeSelectionKind
// passes an unrecognized value through unchanged, so `--kind bogus` was persisted
// verbatim into the store and produced a selection nothing could interpret --
// stored with no atom_count, silently useless.
func ValidateSelectionKind(value string) error {
	normalized := normalizeSelectionKind(value)
	if normalized == "" || normalized == "raw" {
		return nil
	}
	for _, known := range SelectionKinds {
		if normalized == known {
			return nil
		}
	}
	return fmt.Errorf("unknown selection kind %q: expected one of %s", value, strings.Join(SelectionKinds, ", "))
}

func normalizeSelectionKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "raw":
		return strings.ToLower(strings.TrimSpace(value))
	case "atom", "atoms", "atom_index", "atom-index", "gromacs", "gmx":
		return "atom-index"
	case "mdtraj", "md-traj":
		return "mdtraj"
	case "mdanalysis", "mda", "md-analysis":
		return "mdanalysis"
	case "python":
		return "python"
	case "mvs", "molstar", "mol*", "molstar-mvs":
		return "mvs"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func convertAtomIndexSelection(expression string, target string, atomCount int) (string, error) {
	atoms, err := ParseAtomSelection(expression, atomCount)
	if err != nil {
		return "", err
	}
	switch target {
	case "atom-index", "gromacs", "gmx":
		return expression, nil
	case "mdtraj", "mdanalysis", "python":
		return atomIndicesToPythonSelection(atoms), nil
	case "mvs":
		return atomIndicesToMVSSelection(atoms), nil
	default:
		return "", fmt.Errorf("cannot convert atom-index selection to %q", target)
	}
}

func atomIndicesToPythonSelection(atoms []int) string {
	if len(atoms) == 0 {
		return ""
	}
	atoms = append([]int(nil), atoms...)
	sort.Ints(atoms)
	parts := make([]string, 0, len(atoms))
	for _, atom := range atoms {
		parts = append(parts, "index "+strconv.Itoa(atom))
	}
	return strings.Join(parts, " or ")
}

func atomIndicesToMVSSelection(atoms []int) string {
	if len(atoms) == 0 {
		return ""
	}
	atoms = append([]int(nil), atoms...)
	sort.Ints(atoms)
	parts := make([]string, 0, len(atoms))
	for _, atom := range atoms {
		parts = append(parts, "atom-index:"+strconv.Itoa(atom+1))
	}
	return strings.Join(parts, ",")
}
