package renovate

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// The signal helper runs as the credential it is given and reaches a whole process group.
func TestSignalAsStopsAProcessGroup(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 60 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	cred := &syscall.Credential{Uid: uint32(os.Getuid()), Gid: uint32(os.Getgid()), NoSetGroups: true}
	if err := signalAs(cred, cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("signalAs: %v", err)
	}
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Errorf("the group leader ended with %v, want it signalled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the helper did not stop the process group")
	}
	if err := syscall.Kill(-cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		// The grandchild may take a moment to be reaped.
		time.Sleep(200 * time.Millisecond)
		if err := syscall.Kill(-cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Errorf("the process group is still there: %v", err)
		}
	}
}

func TestSignalAsRefusesAnUnknownSignal(t *testing.T) {
	if err := signalAs(&syscall.Credential{}, 1, syscall.SIGUSR1); err == nil {
		t.Error("an unsupported signal was sent")
	}
}

func TestProcessAttrSwitchesTheUserAndStartsAGroup(t *testing.T) {
	attr := processAttr(User{Name: "renovate", UID: 12031, GID: 0})
	if !attr.Setpgid || attr.Credential == nil || attr.Credential.Uid != 12031 || attr.Credential.Gid != 0 || len(attr.Credential.Groups) != 0 || attr.Credential.NoSetGroups {
		t.Errorf("attr = %+v, credential %+v", attr, attr.Credential)
	}
	if attr := processAttr(User{}); attr.Credential != nil {
		t.Error("a credential for no user")
	}
}
