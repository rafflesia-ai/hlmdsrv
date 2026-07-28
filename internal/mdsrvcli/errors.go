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
	codeInternalError    errorCode = "internal_error"
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
	case codeInvalidManifest:
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
		strings.Contains(message, "python backend failed"),
		strings.Contains(message, "trajectory backend"):
		return codeMissingBackend
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
		strings.Contains(message, "verification failed"):
		return codeValidationFailed
	case strings.Contains(message, "already exists"),
		strings.Contains(message, "pass --force"):
		return codeConflict
	case strings.Contains(message, "render failed"),
		strings.Contains(message, "visualization failed"):
		return codeRenderFailed
	case strings.Contains(message, "not found"),
		strings.Contains(message, "no such file"),
		strings.Contains(message, "is required"):
		return codeMissingInput
	default:
		return codeInternalError
	}
}
