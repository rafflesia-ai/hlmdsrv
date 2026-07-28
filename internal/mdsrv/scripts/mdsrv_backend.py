#!/usr/bin/env python3
import csv
import importlib.util
import json
import math
import os
import re
import sys


def main():
    if len(sys.argv) != 2:
        fail("usage: mdsrv_backend.py COMMAND")
    command = sys.argv[1]
    payload = json.load(sys.stdin)
    if command == "doctor":
        emit(doctor())
    backend = load_backend()
    if command == "info":
        emit(backend.info(payload))
    if command == "frame":
        emit(backend.frame(payload))
    if command == "analyze":
        emit(backend.analyze(payload))
    fail(f"unknown command {command!r}")


def doctor():
    return {
        "python": sys.version.split()[0],
        "mdtraj": importlib.util.find_spec("mdtraj") is not None,
        "MDAnalysis": importlib.util.find_spec("MDAnalysis") is not None,
    }


def load_backend():
    preferred = preferred_backend()
    if preferred == "mdtraj":
        if importlib.util.find_spec("mdtraj") is None:
            fail("requested mdtraj backend is unavailable")
        import mdtraj as md

        return MDTrajBackend(md)
    if preferred == "mdanalysis":
        if importlib.util.find_spec("MDAnalysis") is None:
            fail("requested MDAnalysis backend is unavailable")
        import MDAnalysis as mda

        return MDAnalysisBackend(mda)
    if importlib.util.find_spec("mdtraj") is not None:
        import mdtraj as md

        return MDTrajBackend(md)
    if importlib.util.find_spec("MDAnalysis") is not None:
        import MDAnalysis as mda

        return MDAnalysisBackend(mda)
    fail("trajectory backend unavailable: install mdtraj or MDAnalysis")


def preferred_backend():
    value = os.environ.get("MDSRV_BACKEND", "").strip().lower()
    if value in ("", "python", "auto"):
        return ""
    if value in ("mdtraj", "md-traj"):
        return "mdtraj"
    if value in ("mdanalysis", "mda", "md-analysis"):
        return "mdanalysis"
    fail(f"unsupported requested Python backend {value!r}")


class MDTrajBackend:
    name = "mdtraj"

    def __init__(self, md):
        self.md = md

    def info(self, payload):
        traj = self._load(payload)
        return {
            "backend": self.name,
            "frames": int(traj.n_frames),
            "atoms": int(traj.n_atoms),
            "topology_atoms": int(traj.topology.n_atoms),
            "time_unit": "ps",
            "coordinate_unit": "nm",
            "first_time": float(traj.time[0]) if traj.n_frames else 0.0,
            "last_time": float(traj.time[-1]) if traj.n_frames else 0.0,
            "has_unit_cell": traj.unitcell_vectors is not None,
        }

    def frame(self, payload):
        frame_index = int(payload.get("frame", 0))
        atom_indices = self._atom_indices(payload)
        traj = self.md.load_frame(
            payload["trajectory"],
            frame_index,
            top=payload["topology"],
            atom_indices=atom_indices,
        )
        unit_cell = unit_cell_vectors(traj.unitcell_vectors)
        coords = traj.xyz[0].astype(float).tolist()
        return {
            "backend": self.name,
            "frame": frame_index,
            "time": float(traj.time[0]) if len(traj.time) else 0.0,
            "time_unit": "ps",
            "coordinate_unit": "nm",
            "unit_cell": unit_cell,
            "coordinates": coords,
        }

    def analyze(self, payload):
        analysis_type = payload["type"]
        traj = self._load(payload)
        if analysis_type == "rmsd":
            indices = self._select(traj, payload["selection"])
            values = self.md.rmsd(
                traj,
                traj,
                frame=int(payload.get("reference_frame", 0)),
                atom_indices=indices,
            )
            return trace(payload, traj, values.tolist(), "nm")
        if analysis_type == "rgyr":
            indices = self._select(traj, payload["selection"])
            sliced = traj.atom_slice(indices)
            values = self.md.compute_rg(sliced)
            return trace(payload, traj, values.tolist(), "nm")
        if analysis_type == "rmsf":
            indices = self._select(traj, payload["selection"])
            reference = traj[int(payload.get("reference_frame", 0))]
            values = self.md.rmsf(traj, reference, atom_indices=indices)
            return {
                "backend": self.name,
                "id": payload.get("id") or payload.get("type"),
                "type": analysis_type,
                "unit": "nm",
                "values": [
                    {"frame": int(indices[i]), "time": float(indices[i]), "value": float(values[i])}
                    for i in range(len(values))
                ],
            }
        if analysis_type == "contacts":
            selections = ordered_selections(payload, analysis_type)
            a = self._select(traj, selections[0])
            b = self._select(traj, selections[1])
            pairs = [[int(i), int(j)] for i in a for j in b if int(i) != int(j)]
            if not pairs:
                fail("contacts selections produced no atom pairs")
            distances = self.md.compute_distances(traj, pairs)
            cutoff = float(payload.get("cutoff") or 0.5)
            values = (distances <= cutoff).sum(axis=1)
            return trace(payload, traj, values.tolist(), "count")
        if analysis_type == "sasa":
            indices = self._select(traj, payload["selection"])
            sliced = traj.atom_slice(indices)
            values = self.md.shrake_rupley(sliced).sum(axis=1)
            return trace(payload, traj, values.tolist(), "nm^2")
        if analysis_type == "hbonds":
            counts = []
            for i in range(traj.n_frames):
                hbonds = self.md.baker_hubbard(traj[i], periodic=False)
                counts.append(len(hbonds))
            return trace(payload, traj, counts, "count")
        if analysis_type in ("distance", "angle", "dihedral"):
            selections = ordered_selections(payload, analysis_type)
            centers = [selection_centers(traj, self._select(traj, sel)) for sel in selections]
            values = geometry_values(analysis_type, centers)
            unit = "nm" if analysis_type == "distance" else "degree"
            return trace(payload, traj, values, unit)
        fail(f"unsupported analysis type {analysis_type!r}")

    def _load(self, payload):
        stride = int(payload.get("stride") or 1)
        atom_indices = self._atom_indices(payload)
        return self.md.load(
            payload["trajectory"],
            top=payload["topology"],
            stride=max(stride, 1),
            atom_indices=atom_indices,
        )

    def _atom_indices(self, payload):
        selection = payload.get("atom_subset") or ""
        if not selection:
            return None
        topology = self.md.load_topology(payload["topology"])
        parsed = parse_cli_atom_selection(selection, topology.n_atoms)
        if parsed is not None:
            return parsed
        indices = topology.select(selection)
        if len(indices) == 0:
            fail(f"selection {selection!r} matched no atoms")
        return indices

    def _select(self, traj, selection):
        if not selection:
            fail("selection is required")
        parsed = parse_cli_atom_selection(selection, traj.n_atoms)
        if parsed is not None:
            return parsed
        indices = traj.topology.select(selection)
        if len(indices) == 0:
            fail(f"selection {selection!r} matched no atoms")
        return indices


class MDAnalysisBackend:
    name = "MDAnalysis"

    def __init__(self, mda):
        self.mda = mda

    def info(self, payload):
        u = self._universe(payload)
        frames = len(u.trajectory)
        first_time = float(u.trajectory[0].time) if frames else 0.0
        last_time = float(u.trajectory[-1].time) if frames else 0.0
        return {
            "backend": self.name,
            "frames": int(frames),
            "atoms": int(u.atoms.n_atoms),
            "topology_atoms": int(u.atoms.n_atoms),
            "time_unit": "ps",
            "coordinate_unit": "angstrom",
            "first_time": first_time,
            "last_time": last_time,
            "has_unit_cell": True,
        }

    def frame(self, payload):
        frame_index = int(payload.get("frame", 0))
        u = self._universe(payload)
        atoms = self._atoms(u, payload.get("atom_subset") or "all")
        u.trajectory[frame_index]
        return {
            "backend": self.name,
            "frame": frame_index,
            "time": float(u.trajectory.ts.time),
            "time_unit": "ps",
            "coordinate_unit": "angstrom",
            "unit_cell": dimensions_to_vectors(u.trajectory.ts.dimensions),
            "coordinates": atoms.positions.astype(float).tolist(),
        }

    def analyze(self, payload):
        analysis_type = payload["type"]
        u = self._universe(payload)
        times = []
        values = []
        if analysis_type == "distance":
            selections = ordered_selections(payload, analysis_type)
            a = self._atoms(u, selections[0])
            b = self._atoms(u, selections[1])
            for ts in u.trajectory:
                times.append(float(ts.time))
                values.append(distance(center(a.positions), center(b.positions)))
            return trace_from_arrays(payload, times, values, "angstrom")
        if analysis_type in ("angle", "dihedral"):
            selections = ordered_selections(payload, analysis_type)
            groups = [self._atoms(u, sel) for sel in selections]
            for ts in u.trajectory:
                points = [center(group.positions) for group in groups]
                times.append(float(ts.time))
                values.append(geometry_single(analysis_type, points))
            return trace_from_arrays(payload, times, values, "degree")
        if analysis_type == "rmsd":
            atoms = self._atoms(u, payload["selection"])
            reference_frame = int(payload.get("reference_frame", 0))
            u.trajectory[reference_frame]
            reference = atoms.positions.copy()
            for ts in u.trajectory:
                diff = atoms.positions - reference
                times.append(float(ts.time))
                values.append(float(math.sqrt((diff * diff).sum() / atoms.n_atoms)))
            return trace_from_arrays(payload, times, values, "angstrom")
        if analysis_type == "rgyr":
            atoms = self._atoms(u, payload["selection"])
            for ts in u.trajectory:
                times.append(float(ts.time))
                values.append(float(atoms.radius_of_gyration()))
            return trace_from_arrays(payload, times, values, "angstrom")
        if analysis_type == "contacts":
            selections = ordered_selections(payload, analysis_type)
            a = self._atoms(u, selections[0])
            b = self._atoms(u, selections[1])
            cutoff = float(payload.get("cutoff") or 5.0)
            for ts in u.trajectory:
                times.append(float(ts.time))
                count = 0
                for pa in a.positions:
                    for pb in b.positions:
                        if distance(pa, pb) <= cutoff:
                            count += 1
                values.append(float(count))
            return trace_from_arrays(payload, times, values, "count")
        fail(f"unsupported analysis type {analysis_type!r}")

    def _universe(self, payload):
        return self.mda.Universe(payload["topology"], payload["trajectory"])

    def _atoms(self, universe, selection):
        parsed = parse_cli_atom_selection(selection, universe.atoms.n_atoms)
        if parsed is not None:
            atoms = universe.atoms[parsed]
            if atoms.n_atoms == 0:
                fail(f"selection {selection!r} matched no atoms")
            return atoms
        atoms = universe.select_atoms(selection)
        if atoms.n_atoms == 0:
            fail(f"selection {selection!r} matched no atoms")
        return atoms


def ordered_selections(payload, analysis_type):
    selections = payload.get("selections") or {}
    keys = {"distance": ["a", "b"], "contacts": ["a", "b"], "angle": ["a", "b", "c"], "dihedral": ["a", "b", "c", "d"]}[analysis_type]
    values = [selections.get(key, "") for key in keys]
    if any(not value for value in values):
        fail(f"{analysis_type} analysis requires selections {', '.join(keys)}")
    return values


def parse_cli_atom_selection(selection, atom_count):
    value = str(selection or "").strip().lower()
    if not value:
        return None
    had_prefix = False
    for prefix in ("atom:", "atoms:", "index:", "indices:", "atom ", "atoms ", "index ", "indices "):
        if value.startswith(prefix):
            candidate = value[len(prefix) :].strip()
            if candidate == "all" or re.fullmatch(r"[0-9,\-\s]+", candidate):
                value = candidate
                had_prefix = True
                break
            return None
    if not had_prefix and value != "all" and not re.fullmatch(r"[0-9,\-\s]+", value):
        return None
    if value == "all":
        return list(range(atom_count))
    result = []
    seen = set()
    for raw_part in value.split(","):
        part = raw_part.strip()
        if not part:
            continue
        if "-" in part:
            start_text, end_text = part.split("-", 1)
            start = int(start_text.strip())
            end = int(end_text.strip())
            if end < start:
                fail(f"invalid descending range {start}-{end}")
            values = range(start, end + 1)
        else:
            values = [int(part)]
        for index in values:
            if index < 1 or index > atom_count:
                fail(f"atom index {index} out of range 1..{atom_count}")
            zero_based = index - 1
            if zero_based not in seen:
                result.append(zero_based)
                seen.add(zero_based)
    if not result:
        fail(f"selection {selection!r} matched no atoms")
    return result


def selection_centers(traj, indices):
    xyz = traj.xyz[:, indices, :]
    return xyz.mean(axis=1)


def geometry_values(kind, centers):
    return [geometry_single(kind, [centers[i][frame] for i in range(len(centers))]) for frame in range(len(centers[0]))]


def geometry_single(kind, points):
    if kind == "distance":
        return distance(points[0], points[1])
    if kind == "angle":
        return angle(points[0], points[1], points[2])
    if kind == "dihedral":
        return dihedral(points[0], points[1], points[2], points[3])
    fail(f"unsupported geometry kind {kind!r}")


def distance(a, b):
    return float(math.sqrt(sum((float(a[i]) - float(b[i])) ** 2 for i in range(3))))


def angle(a, b, c):
    ba = [float(a[i]) - float(b[i]) for i in range(3)]
    bc = [float(c[i]) - float(b[i]) for i in range(3)]
    return math.degrees(vector_angle(ba, bc))


def dihedral(a, b, c, d):
    b0 = [float(a[i]) - float(b[i]) for i in range(3)]
    b1 = [float(c[i]) - float(b[i]) for i in range(3)]
    b2 = [float(d[i]) - float(c[i]) for i in range(3)]
    b1n = normalize(b1)
    v = subtract(b0, scale(b1n, dot(b0, b1n)))
    w = subtract(b2, scale(b1n, dot(b2, b1n)))
    x = dot(v, w)
    y = dot(cross(b1n, v), w)
    return math.degrees(math.atan2(y, x))


def vector_angle(a, b):
    denom = math.sqrt(dot(a, a) * dot(b, b))
    if denom == 0:
        fail("cannot compute angle with a zero-length vector")
    value = max(-1.0, min(1.0, dot(a, b) / denom))
    return math.acos(value)


def normalize(v):
    length = math.sqrt(dot(v, v))
    if length == 0:
        fail("cannot normalize zero-length vector")
    return [x / length for x in v]


def dot(a, b):
    return sum(float(a[i]) * float(b[i]) for i in range(3))


def cross(a, b):
    return [
        a[1] * b[2] - a[2] * b[1],
        a[2] * b[0] - a[0] * b[2],
        a[0] * b[1] - a[1] * b[0],
    ]


def subtract(a, b):
    return [float(a[i]) - float(b[i]) for i in range(3)]


def scale(a, factor):
    return [float(x) * factor for x in a]


def center(points):
    count = len(points)
    return [sum(float(point[i]) for point in points) / count for i in range(3)]


def trace(payload, traj, values, unit):
    times = [float(value) for value in traj.time[: len(values)]]
    return trace_from_arrays(payload, times, values, unit)


def trace_from_arrays(payload, times, values, unit):
    return {
        "backend": load_backend_name(),
        "id": payload.get("id") or payload.get("type"),
        "type": payload["type"],
        "unit": unit,
        "values": [
            {"frame": int(i), "time": float(times[i]), "value": float(values[i])}
            for i in range(len(values))
        ],
    }


def load_backend_name():
    preferred = preferred_backend()
    if preferred == "mdtraj":
        return "mdtraj"
    if preferred == "mdanalysis":
        return "MDAnalysis"
    if importlib.util.find_spec("mdtraj") is not None:
        return "mdtraj"
    if importlib.util.find_spec("MDAnalysis") is not None:
        return "MDAnalysis"
    return ""


def unit_cell_vectors(values):
    if values is None:
        return [[0.0, 0.0, 0.0], [0.0, 0.0, 0.0], [0.0, 0.0, 0.0]]
    return values[0].astype(float).tolist()


def dimensions_to_vectors(dimensions):
    if dimensions is None or len(dimensions) < 3:
        return [[0.0, 0.0, 0.0], [0.0, 0.0, 0.0], [0.0, 0.0, 0.0]]
    return [
        [float(dimensions[0]), 0.0, 0.0],
        [0.0, float(dimensions[1]), 0.0],
        [0.0, 0.0, float(dimensions[2])],
    ]


def emit(value):
    json.dump(value, sys.stdout, separators=(",", ":"))
    sys.stdout.write("\n")
    raise SystemExit(0)


def fail(message):
    sys.stderr.write(str(message) + "\n")
    raise SystemExit(1)


if __name__ == "__main__":
    main()
