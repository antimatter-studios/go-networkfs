package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMinutes(t *testing.T) {
	for _, test := range []struct {
		name     string
		entry    budget
		expected int
		invalid  bool
	}{
		{name: "minimum", entry: measured(100, 120), expected: 5},
		{name: "exact boundary", entry: measured(200), expected: 5},
		{name: "round up", entry: measured(201), expected: 6},
		{name: "average not maximum", entry: measured(100, 300), expected: 5},
		{name: "fuzz", entry: measured(2428), expected: 61},
		{name: "matrix maximum", entry: budget{Samples: map[string][]sample{"linux": {{RunID: 1, Seconds: 100}}, "macos": {{RunID: 2, Seconds: 400}}}}, expected: 10},
		{name: "provisional", entry: budget{Provisional: true}, expected: 5},
		{name: "provisional with samples", entry: budget{Provisional: true, Samples: measured(100).Samples}, invalid: true},
		{name: "missing", invalid: true},
		{name: "empty variant", entry: budget{Samples: map[string][]sample{"linux": {}}}, invalid: true},
		{name: "negative", entry: measured(-1), invalid: true},
		{name: "missing source", entry: budget{Samples: map[string][]sample{"linux": {{Seconds: 20}}}}, invalid: true},
		{name: "platform limit", entry: measured(14401), invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, err := minutes(test.entry)
			if (err != nil) != test.invalid || actual != test.expected {
				t.Fatalf("minutes = %d, %v; want %d, invalid=%v", actual, err, test.expected, test.invalid)
			}
		})
	}
}

func measured(seconds ...int) budget {
	samples := make([]sample, len(seconds))
	for index, duration := range seconds {
		samples[index] = sample{RunID: int64(index + 1), Seconds: duration}
	}
	return budget{Samples: map[string][]sample{"default": samples}}
}

func TestCheckJob(t *testing.T) {
	pinned := "owner/core/.github/workflows/reap.yml@17e99aad9186bf14dbc78987ebc341a4810c19e0"
	reusable := measured(10)
	reusable.ReusableWorkflow = pinned
	reusable.ReusableTimeoutMinutes = 5
	for _, test := range []struct {
		name    string
		job     workflowJob
		entry   budget
		invalid bool
	}{
		{name: "bounded", job: workflowJob{Timeout: 5}, entry: measured(100)},
		{name: "missing timeout", entry: measured(100), invalid: true},
		{name: "inflated timeout", job: workflowJob{Timeout: 60}, entry: measured(100), invalid: true},
		{name: "bad evidence", job: workflowJob{Timeout: 5}, invalid: true},
		{name: "pinned reusable", job: workflowJob{Uses: pinned}, entry: reusable},
		{name: "unpinned reusable", job: workflowJob{Uses: "owner/core/.github/workflows/reap.yml@main"}, entry: reusable, invalid: true},
		{name: "changed reusable", job: workflowJob{Uses: "other/" + pinned}, entry: reusable, invalid: true},
		{name: "caller timeout", job: workflowJob{Uses: pinned, Timeout: 5}, entry: reusable, invalid: true},
		{name: "stale reusable", job: workflowJob{Timeout: 5}, entry: reusable, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := checkJob(test.job, test.entry); (err != nil) != test.invalid {
				t.Fatalf("checkJob = %v; invalid=%v", err, test.invalid)
			}
		})
	}
}

func TestCheckWorkflows(t *testing.T) {
	for _, test := range []struct {
		name    string
		files   map[string]string
		entries map[string]map[string]budget
		invalid bool
	}{
		{name: "all extensions", files: map[string]string{"ci.yml": "jobs:\n  test:\n    timeout-minutes: 5\n", "nightly.yaml": "jobs:\n  fuzz:\n    timeout-minutes: 5\n"}, entries: map[string]map[string]budget{"ci.yml": {"test": measured(10)}, "nightly.yaml": {"fuzz": measured(10)}}},
		{name: "step timeout not job timeout", files: map[string]string{"ci.yml": "jobs:\n  test:\n    steps:\n      - timeout-minutes: 5\n"}, entries: map[string]map[string]budget{"ci.yml": {"test": measured(10)}}, invalid: true},
		{name: "new job", files: map[string]string{"ci.yml": "jobs:\n  test:\n    timeout-minutes: 5\n"}, invalid: true},
		{name: "stale job", files: map[string]string{"ci.yml": "jobs:\n  test:\n    timeout-minutes: 5\n"}, entries: map[string]map[string]budget{"ci.yml": {"test": measured(10), "removed": measured(10)}}, invalid: true},
		{name: "stale workflow", files: map[string]string{"ci.yml": "jobs:\n  test:\n    timeout-minutes: 5\n"}, entries: map[string]map[string]budget{"ci.yml": {"test": measured(10)}, "removed.yml": {}}, invalid: true},
		{name: "no jobs", files: map[string]string{"ci.yml": "jobs: {}"}, invalid: true},
		{name: "no workflows", invalid: true},
		{name: "expression timeout", files: map[string]string{"ci.yml": "jobs:\n  test:\n    timeout-minutes: ${{ matrix.timeout }}\n"}, invalid: true},
		{name: "invalid yaml", files: map[string]string{"ci.yml": "jobs: ["}, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			workflows := filepath.Join(root, ".github", "workflows")
			if err := os.MkdirAll(workflows, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, content := range test.files {
				if err := os.WriteFile(filepath.Join(workflows, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			content, err := json.Marshal(evidence{Workflows: test.entries})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".github", "job-budgets.json"), content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := check(root); (err != nil) != test.invalid {
				t.Fatalf("check = %v; invalid=%v", err, test.invalid)
			}
		})
	}
}
