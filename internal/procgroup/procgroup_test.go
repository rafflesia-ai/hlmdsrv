package procgroup

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestIsolateSetsGroupAndCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX process groups on Windows")
	}
	cmd := exec.Command("true")
	Isolate(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatalf("Isolate must set Setpgid, got %+v", cmd.SysProcAttr)
	}
	if cmd.Cancel == nil {
		t.Fatal("Isolate must set a group-killing Cancel func")
	}
	// Cancel before start (Process==nil) must be a harmless no-op, not a panic.
	if err := cmd.Cancel(); err != nil {
		t.Fatalf("Cancel before start = %v, want nil", err)
	}
}

// TestIsolateSetsWaitDelay asserts Isolate installs a non-zero WaitDelay. Zero
// (the default) means Wait/CombinedOutput blocks forever whenever a pipe write-end
// stays open — the wedge that let a stuck fpocket/qhull child hang the CLI even
// under an external SIGTERM.
func TestIsolateSetsWaitDelay(t *testing.T) {
	cmd := exec.Command("true")
	Isolate(cmd)
	if cmd.WaitDelay <= 0 {
		t.Fatalf("Isolate must set a positive WaitDelay, got %v", cmd.WaitDelay)
	}
}

// TestWaitDelayUnblocksHeldPipe is the functional regression test: a child that
// exits while a backgrounded grandchild keeps the stdout pipe open must not hang
// CombinedOutput forever. Without WaitDelay this call never returns; with it,
// exec closes the pipes after the grace window and returns. cancelGrace is shrunk
// so the test is fast.
func TestWaitDelayUnblocksHeldPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell grandchild scenario is POSIX-specific")
	}
	orig := cancelGrace
	cancelGrace = 300 * time.Millisecond
	defer func() { cancelGrace = orig }()

	// The direct child exits immediately, but forks a grandchild that holds the
	// inherited stdout open for far longer than the grace window.
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "sleep 30 & exit 0")
	Isolate(cmd)

	done := make(chan struct{})
	start := time.Now()
	go func() {
		_, _ = cmd.CombinedOutput()
		close(done)
	}()

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("CombinedOutput took %v, want ~cancelGrace (WaitDelay not effective)", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CombinedOutput hung past 5s despite WaitDelay; a held pipe still wedges Wait")
	}
}
