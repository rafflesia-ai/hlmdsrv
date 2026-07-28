//go:build windows

package procgroup

import "os/exec"

// isolate is a no-op on Windows, which has no POSIX process groups; the default
// exec.CommandContext cancellation (killing the direct child) is retained. A Job
// Object could contain grandchildren but is out of scope for these headless
// tools, which run on Unix in practice.
func isolate(cmd *exec.Cmd) {}
