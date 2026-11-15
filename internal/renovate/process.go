package renovate

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// startMu serializes process starts: the group-writable umask is set around Start so what
// Renovate creates can be removed by the worker, and the umask is the process's.
var startMu sync.Mutex

type processOutcome struct {
	// exit is the exit status; nil when the process was signalled or never ran.
	exit      *int
	startErr  error
	timedOut  bool
	cancelled bool
}

// runProcess starts cmd and waits for it to end, for ctx to end or for runBy. On the latter
// two it stops the process group with SIGTERM and, after the grace, SIGKILL, by process id.
// Whatever remains of the group afterwards is killed too, so nothing outlives the job.
func (h *Handler) runProcess(ctx context.Context, cmd *exec.Cmd, runBy time.Time, as User) processOutcome {
	startMu.Lock()
	old := syscall.Umask(0o002)
	err := cmd.Start()
	syscall.Umask(old)
	startMu.Unlock()
	if err != nil {
		return processOutcome{startErr: err}
	}
	pid := cmd.Process.Pid

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(runBy.Sub(h.opts.Now()))
	defer timer.Stop()

	var out processOutcome
	var waitErr error
	select {
	case waitErr = <-done:
	case <-timer.C:
		out.timedOut = true
		waitErr = h.stop(pid, done, as)
	case <-ctx.Done():
		out.cancelled = true
		waitErr = h.stop(pid, done, as)
	}
	h.killGroup(pid, syscall.SIGKILL, as)

	if !out.timedOut && !out.cancelled && cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		code := cmd.ProcessState.ExitCode()
		out.exit = &code
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) && !errors.Is(waitErr, exec.ErrWaitDelay) {
		h.log.Warn("renovate: waiting for the process", "err", waitErr)
	}
	return out
}

// stop ends the process group: SIGTERM, then SIGKILL after the grace.
func (h *Handler) stop(pid int, done <-chan error, as User) error {
	h.killGroup(pid, syscall.SIGTERM, as)
	grace := time.NewTimer(h.opts.KillGrace)
	defer grace.Stop()
	select {
	case err := <-done:
		return err
	case <-grace.C:
	}
	h.killGroup(pid, syscall.SIGKILL, as)
	return <-done
}

// killGroup signals the process group of pid. A group that is already gone is not an error.
// The worker does not hold CAP_KILL, so a signal to a process of another user is refused;
// then the signal is sent by a short-lived helper started as that user, through the same
// credential switch the worker uses to start Renovate.
func (h *Handler) killGroup(pid int, sig syscall.Signal, as User) {
	err := syscall.Kill(-pid, sig)
	if errors.Is(err, syscall.EPERM) && as.Name != "" {
		err = signalAs(credential(as), pid, sig)
	}
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		h.log.Warn("renovate: signalling the process group failed", "signal", sig.String(), "err", err)
	}
}

const signalHelperTimeout = 5 * time.Second

var signalNames = map[syscall.Signal]string{syscall.SIGTERM: "TERM", syscall.SIGKILL: "KILL"}

// signalAs sends sig to the process group of pid from a process running as cred's user.
func signalAs(cred *syscall.Credential, pid int, sig syscall.Signal) error {
	name, ok := signalNames[sig]
	if !ok {
		return fmt.Errorf("signal %d is not supported", sig)
	}
	ctx, cancel := context.WithTimeout(context.Background(), signalHelperTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `kill -s "$1" -- "-$2"`, "sh", name, strconv.Itoa(pid))
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("signal helper: %w", err)
	}
	return nil
}
