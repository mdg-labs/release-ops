package renovate

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/renovate/redact"
)

// readReportWithin fails the test instead of hanging it when readReport blocks.
func readReportWithin(t *testing.T, path string) (status, report string) {
	t.Helper()
	type result struct{ status, report string }
	done := make(chan result, 1)
	go func() {
		s, r := readReport(path, redact.New("secret-token"))
		done <- result{s, r}
	}()
	select {
	case r := <-done:
		return r.status, r.report
	case <-time.After(5 * time.Second):
		t.Fatal("readReport blocked")
		return "", ""
	}
}

func TestReadReportOnlyReadsRegularFiles(t *testing.T) {
	dir := t.TempDir()

	t.Run("a FIFO with no writer is unreadable", func(t *testing.T) {
		p := filepath.Join(dir, "fifo")
		if err := syscall.Mkfifo(p, 0o600); err != nil {
			t.Skipf("no FIFOs here: %v", err)
		}
		if status, report := readReportWithin(t, p); status != queue.ReportUnreadable || report != "" {
			t.Errorf("status = %q, report = %q", status, report)
		}
	})
	t.Run("a directory is unreadable", func(t *testing.T) {
		p := filepath.Join(dir, "dir")
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if status, report := readReportWithin(t, p); status != queue.ReportUnreadable || report != "" {
			t.Errorf("status = %q, report = %q", status, report)
		}
	})
	t.Run("a missing file is missing", func(t *testing.T) {
		if status, _ := readReportWithin(t, filepath.Join(dir, "none")); status != queue.ReportMissing {
			t.Errorf("status = %q", status)
		}
	})
	t.Run("a regular file is read and redacted", func(t *testing.T) {
		p := filepath.Join(dir, "report.json")
		if err := os.WriteFile(p, []byte(`{"t":"secret-token"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		status, report := readReportWithin(t, p)
		if status != queue.ReportOK || report == "" || report == `{"t":"secret-token"}` {
			t.Errorf("status = %q, report = %q", status, report)
		}
	})
}
