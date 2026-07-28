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
	switch {
	case strings.Contains(message, "schema validation failed"),
		strings.Contains(message, "decode yaml"),
		strings.Contains(message, "decode json"),
		strings.Contains(message, "version is required"),
		strings.Contains(message, "metadata.id is required"),
		strings.Contains(message, "job requires at least one trajectory"):
		return codeInvalidManifest
	case strings.Contains(message, "gromacs command") && strings.Contains(message, "not found"),
		strings.Contains(message, "gromacs command") && strings.Contains(message, "not usable"),
		// Match the specific unavailability signals, NOT the generic "python backend
		// failed" prefix that every python-bridge error carries. That prefix made an
		// argument mistake ("selection is required") report as a missing backend,
		// telling the caller to install MDTraj while MDTraj was installed and working.
		strings.Contains(message, "trajectory backend unavailable"),
		strings.Contains(message, "install mdtraj"),
		strings.Contains(message, "no python interpreter"),
		strings.Contains(message, "python backend is unavailable"),
		// A remote --server that cannot be reached is a backend that is not
		// available, not an unclassified internal fault. Without this, pointing
		// `jobs` at a dead server reported internal_error, whose documented meaning
		// tells the caller to file a bug rather than check the address.
		strings.Contains(message, "connection refused"),
		strings.Contains(message, "no such host"),
		strings.Contains(message, "network is unreachable"),
		strings.Contains(message, "dial tcp"):
		return codeMissingBackend
	// A path that exists but is the wrong kind of thing, or an output the caller
	// pointed somewhere unusable. These are fixable at the call site, so they must
	// not fall through to internal_error, which the error table reserves for
	// "unclassified, report it".
	case strings.Contains(message, "not a directory"),
		strings.Contains(message, "is a directory"),
		strings.Contains(message, "is not a regular file"),
		// Explicit flag-range validation ("--frames must be at least 2",
		// "--workers cannot be negative"). These are returned as plain errors from
		// RunE, so without a pattern they land in internal_error and tell the caller
		// to file a bug about their own typo. The "--x is required" variants are
		// already covered by the missing_input branch below.
		strings.Contains(message, "must be at least"),
		strings.Contains(message, "must be greater"),
		strings.Contains(message, "must be positive"),
		strings.Contains(message, "cannot be negative"),
		// A file handed to `unpack` that is not a zip.
		strings.Contains(message, "is not a valid"),
		// Selection expressions and dialects the caller can correct.
		strings.Contains(message, "out of range"),
		strings.Contains(message, "invalid atom index selection"),
		strings.Contains(message, "invalid descending range"),
		strings.Contains(message, "unknown selection kind"),
		strings.Contains(message, "cannot convert"),
		// Analysis arguments the caller can supply: a required selection, or an
		// analysis the chosen fallback does not implement.
		strings.Contains(message, "selection is required"),
		strings.Contains(message, "requires selections"),
		strings.Contains(message, "unsupported gromacs fallback"):
		return codeInvalidInput
	case strings.Contains(message, "deadline exceeded"),
		strings.Contains(message, "timed out"),
		strings.Contains(message, "timeout"):
		return codeBackendTimeout
	case strings.Contains(message, "escapes store root"),
		strings.Contains(message, "outside allowed"),
		strings.Contains(message, "outside the source store"),
		strings.Contains(message, "path escapes"):
		return codeUnsafePath
	case strings.Contains(message, "validation failed"),
		strings.Contains(message, "verification failed"),
		// A check that ran and did not pass (compat check) is a validation outcome,
		// not an unclassified fault.
		strings.Contains(message, "check failed"),
		// A resource limit doing its job. runtime.max_atoms/max_frames/max_chunk_bytes
		// exist to bound a job for CI or untrusted input, so tripping one is the
		// policy working, not an unclassified fault — it was landing in
		// internal_error, which tells the caller to report a bug.
		strings.Contains(message, "exceeding max_"):
		return codeValidationFailed
	case strings.Contains(message, "already exists"),
		strings.Contains(message, "pass --force"):
		return codeConflict
	case strings.Contains(message, "render failed"),
		strings.Contains(message, "visualization failed"):
		return codeRenderFailed
	case strings.Contains(message, "not found"),
		strings.Contains(message, "no such file"),
		strings.Contains(message, "does not exist"),
		strings.Contains(message, "is required"):
		return codeMissingInput
	default:
		return codeInternalError
	}
}
