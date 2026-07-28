// Package procgroup gives an exec.Cmd its own process group and, on context
// cancellation, kills that whole group rather than only the direct child.
//
// The fleet's tool runners build children with exec.CommandContext, whose default
// cancel behavior sends a signal to the direct child PID alone. That leaks any
// GRANDCHILDREN an interpreter/wrapper tool spawns — e.g. `python -m pdbfixer`,
// `phenix.molprobity`, or the mafft shell script each fork their real workers, so
// a SIGINT/SIGTERM that terminates the launcher orphans those workers (reparented
// to PID 1) and can leave an inherited pipe open. Isolate closes that gap without
// restructuring each runner's capture flow: it only sets the child's process
// group and overrides exec.Cmd.Cancel.
package procgroup

import (
	"os/exec"
	"time"
)

// cancelGrace bounds how long Wait/CombinedOutput blocks after a cancellation (or
// after the child has exited) before exec force-terminates the child and closes
// its I/O pipes. Without it, WaitDelay is zero and Wait blocks FOREVER whenever a
// pipe write-end stays open — the exact wedge a group SIGKILL cannot clear when a
// child is stuck in an unkillable kernel state (observed: a broken fpocket/qhull
// build leaves a UE — uninterruptible+exiting — process holding stdout), or when
// a surviving grandchild still holds an inherited pipe. Ten seconds is ample for a
// healthy child's pipes to drain after the group is killed; the delay only ever
// elapses when something is genuinely stuck, in which case returning control to
// the caller (as canceled/runtime) beats hanging indefinitely.
//
// A var (not const) only so tests can shrink it; production code never reassigns it.
var cancelGrace = 10 * time.Second

// Isolate configures cmd so that a context cancellation kills the child AND every
// process it spawned, and so that Wait cannot block indefinitely on a child that
// refuses to exit or leaves its I/O pipes open (see cancelGrace). It must be
// called after exec.CommandContext(ctx, ...) and before cmd is started
// (Run/Output/CombinedOutput). The group kill is a no-op on platforms without
// POSIX process groups, but the WaitDelay guard applies everywhere. Safe to call
// on a single-process tool: the group then contains just that one process.
func Isolate(cmd *exec.Cmd) {
	// WaitDelay is a portable exec.Cmd field (Go 1.20+), independent of process
	// groups, so set it here rather than in the per-platform isolate(): it unblocks
	// a pipe-holding grandchild even on Windows, where the group kill is absent.
	cmd.WaitDelay = cancelGrace
	isolate(cmd)
}
