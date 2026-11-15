package hooks_test

import (
	"database/sql"
	"reflect"
	"testing"
)

type webhookDep struct{ Name, Manager, From, To, Type, Source string }

func (r *prRig) deps(t *testing.T, number int64) []webhookDep {
	t.Helper()
	rows, err := r.db.Reader.Query(`SELECT d.name, COALESCE(d.manager, ''), COALESCE(d.from_version, ''), COALESCE(d.to_version, ''),
		COALESCE(d.update_type, ''), d.source FROM update_pr_dependencies d JOIN update_prs p ON p.id = d.update_pr_id
		WHERE p.number = ? ORDER BY d.rowid`, number)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []webhookDep
	for rows.Next() {
		var d webhookDep
		if err := rows.Scan(&d.Name, &d.Manager, &d.From, &d.To, &d.Type, &d.Source); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func (r *prRig) releaseNotes(t *testing.T, number int64) (string, bool) {
	t.Helper()
	var n sql.NullString
	if err := r.db.Reader.QueryRow(`SELECT release_notes FROM update_prs WHERE number = ?`, number).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n.String, n.Valid
}

// storeReport stores a finished job's Renovate report for the fixture repository.
func (r *prRig) storeReport(t *testing.T, report string) {
	t.Helper()
	var repo string
	if err := r.db.Reader.QueryRow(`SELECT id FROM repositories WHERE full_name = 'acme/widgets'`).Scan(&repo); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Writer.Exec(`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, "trigger", created_at)
		VALUES ('job-r', 'renovate_run', ?, 'normal', 'succeeded', '2026-11-15T09:00:00.000Z', 'schedule', '2026-11-15T09:00:00.000Z')`, repo); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Writer.Exec(`INSERT INTO job_runs (id, job_id, attempt, started_at, outcome, report_status, report)
		VALUES ('run-r', 'job-r', 1, '2026-11-15T09:00:00.000Z', 'succeeded', 'ok', ?)`, report); err != nil {
		t.Fatal(err)
	}
}

func TestPullRequestWebhookRecordsTheDependenciesFromTheBodyTable(t *testing.T) {
	r := newPRRig(t, botUserID)
	r.must(t, "g-1", "update_pr_opened.json")
	want := []webhookDep{{Name: "lodash", From: "4.17.20", To: "4.17.21", Source: "body"}}
	if got := r.deps(t, 21); !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies = %+v, want %+v", got, want)
	}
	if _, has := r.releaseNotes(t, 21); has {
		t.Fatal("a body without a release-notes section stored an excerpt")
	}
	r.woken()
	r.must(t, "g-2", "update_pr_opened.json") // the same content again
	if got := r.deps(t, 21); !reflect.DeepEqual(got, want) || r.woken() {
		t.Fatalf("a replay changed the dependencies (%+v) or woke the engine", got)
	}
}

func TestPullRequestWebhookPrefersTheStoredReportAtTheHeadCommit(t *testing.T) {
	r := newPRRig(t, botUserID)
	r.storeReport(t, `{"repositories":{"acme/widgets":{"branches":[{"branchName":"renovate/lodash-4.x","prNo":21,"sha":"`+firstSHA+`","upgrades":[
		{"depName":"lodash","manager":"npm","datasource":"npm","currentVersion":"4.17.20","newVersion":"4.17.21","updateType":"patch"}]}]}}}`)
	r.must(t, "g-1", "update_pr_opened.json")
	want := []webhookDep{{Name: "lodash", Manager: "npm", From: "4.17.20", To: "4.17.21", Type: "patch", Source: "report"}}
	if got := r.deps(t, 21); !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies = %+v, want %+v", got, want)
	}
	// A push moves the head past the report's commit: the body is the fresher source.
	r.must(t, "g-2", "update_pr_synchronize.json")
	want = []webhookDep{{Name: "lodash", From: "4.17.20", To: "4.17.21", Source: "body"}}
	if got := r.deps(t, 21); !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies after a push = %+v, want %+v", got, want)
	}
}

func TestPullRequestWebhookGivesConfigMigrationAndOnboardingPRsNoDependencies(t *testing.T) {
	r := newPRRig(t, botUserID)
	for i, file := range []string{"update_pr_config_migration.json", "update_pr_onboarding.json"} {
		r.must(t, "g-"+file, file)
		number := int64(30 + i)
		if _, ok := r.row(t, number); !ok {
			t.Fatalf("%s: the PR is not tracked", file)
		}
		if got := r.deps(t, number); len(got) != 0 {
			t.Fatalf("%s: dependencies = %+v", file, got)
		}
	}
}

func TestPullRequestWebhookRefreshesTheReleaseNotesWhenThePRIsEdited(t *testing.T) {
	r := newPRRig(t, botUserID)
	r.must(t, "g-1", "update_pr_opened.json")
	r.must(t, "g-2", "update_pr_edited_release_notes.json")
	want := "<details>\n<summary>lodash/lodash (lodash)</summary>\n\n#### 4.17.21\n\n- fixed {{ .Title }} <b>bold</b>\n</details>"
	if got, has := r.releaseNotes(t, 21); !has || got != want {
		t.Fatalf("release notes = %q (%v), want %q", got, has, want)
	}
	// The earlier edit, delivered late, neither clears the excerpt nor rewrites it.
	r.must(t, "g-3", "update_pr_edited.json")
	if got, _ := r.releaseNotes(t, 21); got != want {
		t.Fatalf("a stale edit rewrote the release notes: %q", got)
	}
}
