// Package gromacs is the reusable adapter boundary for invoking GROMACS.
//
// It owns command discovery, execution, typed availability errors, gmx check
// parsing, frame-time mapping, conversion, extraction, export, and tiny demo
// trajectory generation. Callers such as MDsrv should pass high-level options
// into this package and consume typed reports/errors instead of constructing
// gmx commands themselves.
package gromacs
