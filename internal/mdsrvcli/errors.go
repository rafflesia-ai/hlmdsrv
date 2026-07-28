package mdsrvcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

type errorCode string

const (
	codeInternalError errorCode = "internal_error"
	// codeInvalidInput covers caller-fixable bad invocation: an unknown flag or
	// subcommand, a bad flag value, or an input path that is missing, a directory,
	// or otherwise unusable. It shares exit 2 with codeInvalidManifest (both mean
	// "fix the call, do not retry") but keeps codeInvalidManifest meaning what the
	// error table says it means: the manifest itself failed to parse or validate.
	codeInvalidInput     errorCode = "invalid_input"
	codeInvalidManifest  errorCode = "invalid_manifest"
	codeMissingInput     errorCode = "missing_input"
	codeMissingBackend   errorCode = "missing_backend"
	codeBackendTimeout   errorCode = "backend_timeout"
	codeUnsafePath       errorCode = "unsafe_path"
	codeValidationFailed errorCode = "validation_failed"
	codeConflict         errorCode = "conflict"
	codeRenderFailed     errorCode = "render_failed"
	codeCanceled         errorCode = "canceled"
)

type CLIError struct {
	Code    errorCode `json:"code"`
	Message string    `json:"message"`
	Err     error     `json:"-"`
}

func (e *CLIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}

func (e *CLIError) Unwrap() error {
	return e.Err
}

func codedErrorf(code errorCode, format string, args ...any) error {
	return &CLIError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func ClassifyError(err error) *CLIError {
	if err == nil {
		return nil
	}
	var coded *CLIError
	if errors.As(err, &coded) {
		if coded.Message == "" && coded.Err != nil {
			coded.Message = coded.Err.Error()
		}
		return coded
	}
	code := classifyErrorCode(err)
	return &CLIError{Code: code, Message: err.Error(), Err: err}
}

func ErrorCode(err error) string {
	coded := ClassifyError(err)
	if coded == nil {
		return ""
	}
	return string(coded.Code)
}

func ExitCode(err error) int {
	coded := ClassifyError(err)
	if coded == nil {
		return 0
	}
	switch coded.Code {
	case codeInvalidInput, codeInvalidManifest:
		return 2
	case codeMissingInput:
		return 3
	case codeMissingBackend:
		return 4
	case codeBackendTimeout:
		return 5
	case codeUnsafePath:
		return 6
	case codeValidationFailed:
		return 7
	case codeConflict:
		return 8
	case codeRenderFailed:
		return 9
	case codeCanceled:
		return 130
	default:
		return 1
	}
}

// classificationRule maps a family of error messages to a code. Rules are
// evaluated in order and the first match wins, so ORDER IS PART OF THE CONTRACT:
// a broad rule placed above a narrow one silently swallows it. That has happened
// twice while this table was being built, which is why the ordering is data here
// rather than the shape of a switch — the sequence can be read, named, and
// asserted in tests instead of inferred from indentation.
type classificationRule struct {
	// name identifies the rule in failures and in the priority tests.
	name  string
	code  errorCode
	match func(message string) bool
}

func contains(needles ...string) func(string) bool {
	return func(message string) bool {
		for _, needle := range needles {
			if strings.Contains(message, needle) {
				return true
			}
		}
		return false
	}
}

func containsAll(needles ...string) func(string) bool {
	return func(message string) bool {
		for _, needle := range needles {
			if !strings.Contains(message, needle) {
				return false
			}
		}
		return true
	}
}

// classificationRules is ordered most-specific to least. The comments explain why
// each sits where it does; moving one is a behaviour change.
var classificationRules = []classificationRule{
	// A Go runtime panic is definitionally our bug, never the caller's. It leads
	// because the rules below match substrings and some fragments ("out of range")
	// also occur in panic text: without this a real defect would be reported as the
	// caller's invalid_input, which is the worst direction to be wrong.
	{"runtime-panic", codeInternalError, contains("runtime error:", "nil pointer dereference")},

	// Manifest/store data written by an incompatible version, and manifest shape
	// failures. Above the generic "unsupported " rule, which would otherwise claim
	// the version variants.
	{"manifest-shape", codeInvalidManifest, contains(
		"schema validation failed", "decode yaml", "decode json",
		"version is required", "metadata.id is required",
		"job requires at least one trajectory")},
	{"manifest-version", codeInvalidManifest, contains(
		"unsupported manifest version", "unsupported store version", "unsupported job version")},

	// An engine or remote server that cannot run at all. Matches the specific
	// unavailability signals, never the generic "python backend failed" prefix that
	// rides on every python-bridge error, which would turn an argument mistake into
	// "install MDTraj".
	{"gromacs-unusable", codeMissingBackend, func(m string) bool {
		return strings.Contains(m, "gromacs command") &&
			(strings.Contains(m, "not found") || strings.Contains(m, "not usable"))
	}},
	{"python-unavailable", codeMissingBackend, contains(
		"trajectory backend unavailable", "install mdtraj",
		"no python interpreter", "python backend is unavailable")},
	{"server-unreachable", codeMissingBackend, contains(
		"connection refused", "no such host", "network is unreachable", "dial tcp")},

	// The caller supplied something wrong: a path of the wrong kind, a bad flag
	// value, a malformed id or selection, or a choice the tool does not implement.
	{"path-wrong-kind", codeInvalidInput, contains(
		"not a directory", "is a directory", "is not a regular file", "is not a valid")},
	{"flag-range", codeInvalidInput, contains(
		"must be at least", "must be greater", "must be positive", "cannot be negative")},
	// Anchored on our own wording: bare "out of range" and "cannot convert" also
	// belong to the Go runtime and reflection.
	{"selection-malformed", codeInvalidInput, func(m string) bool {
		return containsAll("atom index", "out of range")(m) ||
			contains("invalid atom index selection", "invalid descending range",
				"unknown selection kind", "cannot convert atom-index selection")(m)
	}},
	{"id-malformed", codeInvalidInput, contains("invalid id")},
	// Every "unsupported X" here names a caller's choice — analysis type, backend,
	// format, shell, provider, encoding. Below manifest-version, which claims the
	// version spellings first.
	{"unsupported-choice", codeInvalidInput, contains("unsupported ")},

	{"timeout", codeBackendTimeout, contains("deadline exceeded", "timed out", "timeout")},
	{"unsafe-path", codeUnsafePath, contains(
		"escapes store root", "outside allowed", "outside the source store", "path escapes")},
	// A check that ran and did not pass, or a configured resource limit doing its
	// job — outcomes, not faults.
	{"validation", codeValidationFailed, contains(
		"validation failed", "verification failed", "check failed", "exceeding max_")},
	{"conflict", codeConflict, contains("already exists", "pass --force")},
	{"render", codeRenderFailed, contains("render failed", "visualization failed")},

	// Something required is absent, or something named cannot be found. The
	// boundary against invalid_input is "absent" vs "wrong", and it must not depend
	// on whether cobra or our own code noticed.
	{"absent", codeMissingInput, contains(
		"not found", "no such file", "does not exist",
		"is required", "requires selections", "required flag")},

	// Last resort. Both analysis engines failed and nothing more specific matched,
	// so there is no usable backend. It must stay below every specific rule: placed
	// higher it claimed composites whose fallback merely *refused* the analysis
	// ("unsupported GROMACS fallback analysis"), where the backend ran and is not
	// missing. A typed error never reaches here — ClassifyError unwraps first.
	{"no-analysis-backend", codeMissingBackend, containsAll("python backend failed", "gromacs fallback failed")},
}

func classifyErrorCode(err error) errorCode {
	if errors.Is(err, context.DeadlineExceeded) {
		return codeBackendTimeout
	}
	if errors.Is(err, context.Canceled) {
		return codeCanceled
	}
	if errors.Is(err, os.ErrNotExist) {
		return codeMissingInput
	}
	message := strings.ToLower(err.Error())
	for _, rule := range classificationRules {
		if rule.match(message) {
			return rule.code
		}
	}
	return codeInternalError
}

// classifyRuleName reports which rule claimed a message, for tests that pin the
// priority relationships rather than only the resulting code.
func classifyRuleName(message string) string {
	message = strings.ToLower(message)
	for _, rule := range classificationRules {
		if rule.match(message) {
			return rule.name
		}
	}
	return "default"
}
