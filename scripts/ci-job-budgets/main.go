package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type sample struct {
	RunID   int64 `json:"run_id"`
	Seconds int   `json:"seconds"`
}

type budget struct {
	Samples                map[string][]sample `json:"samples"`
	Provisional            bool                `json:"provisional,omitempty"`
	ReusableWorkflow       string              `json:"reusable_workflow,omitempty"`
	ReusableTimeoutMinutes int                 `json:"reusable_timeout_minutes,omitempty"`
}

type evidence struct {
	MeasuredAt string                       `json:"measured_at"`
	Method     string                       `json:"method"`
	Workflows  map[string]map[string]budget `json:"workflows"`
}

type workflowJob struct {
	Timeout int    `yaml:"timeout-minutes"`
	Uses    string `yaml:"uses"`
}

func minutes(entry budget) (int, error) {
	if entry.Provisional {
		if len(entry.Samples) != 0 {
			return 0, fmt.Errorf("provisional budgets must not contain measurements")
		}
		return 5, nil
	}
	if len(entry.Samples) == 0 {
		return 0, fmt.Errorf("missing successful runtime measurements")
	}
	result := 5
	for variant, samples := range entry.Samples {
		if len(samples) == 0 {
			return 0, fmt.Errorf("%s: empty runtime measurements", variant)
		}
		var total float64
		for _, measurement := range samples {
			if measurement.Seconds < 0 || measurement.RunID <= 0 {
				return 0, fmt.Errorf("%s: invalid runtime measurement", variant)
			}
			total += float64(measurement.Seconds)
		}
		required := int(math.Ceil(total / float64(len(samples)) * 1.5 / 60))
		if required > result {
			result = required
		}
	}
	if result > 360 {
		return 0, fmt.Errorf("calculated budget exceeds GitHub's 360-minute job limit")
	}
	return result, nil
}

func checkJob(root string, job workflowJob, entry budget) error {
	expected, err := minutes(entry)
	if err != nil {
		return err
	}
	if strings.HasPrefix(job.Uses, "./") {
		return checkLocalReusable(root, job, entry, expected)
	}
	if job.Uses != "" {
		if !regexp.MustCompile(`@[a-f0-9]{40}$`).MatchString(job.Uses) || job.Uses != entry.ReusableWorkflow {
			return fmt.Errorf("reusable workflow must match its verified, SHA-pinned budget evidence")
		}
		if job.Timeout != 0 || entry.ReusableTimeoutMinutes != expected {
			return fmt.Errorf("reusable workflow must enforce %d minutes in its called job, not the caller", expected)
		}
		return nil
	}
	if entry.ReusableWorkflow != "" || entry.ReusableTimeoutMinutes != 0 {
		return fmt.Errorf("ordinary job has stale reusable-workflow evidence")
	}
	if job.Timeout != expected {
		return fmt.Errorf("timeout-minutes is %d; measured budget requires %d", job.Timeout, expected)
	}
	return nil
}

// checkLocalReusable verifies a reusable workflow in this repository, `uses:
// ./.github/workflows/<file>.yml`. There is no commit to pin across
// repositories: the callee is the caller's own commit, so the reference in the
// evidence is the path and the limit is verified by reading that file here.
// Every job it defines is called, and each must enforce the same budget.
func checkLocalReusable(root string, job workflowJob, entry budget, expected int) error {
	if job.Timeout != 0 {
		return fmt.Errorf("reusable workflow must enforce %d minutes in its called job, not the caller", expected)
	}
	if job.Uses != entry.ReusableWorkflow || entry.ReusableTimeoutMinutes != expected {
		return fmt.Errorf("reusable workflow must match its verified budget evidence, %d minutes in the called job", expected)
	}
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(job.Uses, "./"))))
	if err != nil {
		return fmt.Errorf("reusable workflow %s: %w", job.Uses, err)
	}
	var called struct {
		Jobs map[string]workflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(content, &called); err != nil {
		return fmt.Errorf("%s: %w", job.Uses, err)
	}
	if len(called.Jobs) == 0 {
		return fmt.Errorf("%s: no jobs found", job.Uses)
	}
	for name, calledJob := range called.Jobs {
		if calledJob.Uses != "" {
			return fmt.Errorf("%s job %s: a nested reusable workflow is not verified", job.Uses, name)
		}
		if calledJob.Timeout != expected {
			return fmt.Errorf("%s job %s: timeout-minutes is %d; measured budget requires %d", job.Uses, name, calledJob.Timeout, expected)
		}
	}
	return nil
}

func check(root string) (int, error) {
	content, err := os.ReadFile(filepath.Join(root, ".github", "job-budgets.json"))
	if err != nil {
		return 0, err
	}
	var recorded evidence
	if err := json.Unmarshal(content, &recorded); err != nil {
		return 0, err
	}
	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*"))
	if err != nil {
		return 0, err
	}
	checked := 0
	for _, path := range paths {
		if !strings.HasSuffix(path, ".yml") && !strings.HasSuffix(path, ".yaml") {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return checked, err
		}
		var workflow struct {
			Jobs map[string]workflowJob `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(content, &workflow); err != nil {
			return checked, fmt.Errorf("%s: %w", path, err)
		}
		name := filepath.Base(path)
		entries := recorded.Workflows[name]
		if len(workflow.Jobs) == 0 {
			return checked, fmt.Errorf("%s: no jobs found", name)
		}
		for name, job := range workflow.Jobs {
			entry, exists := entries[name]
			if !exists {
				return checked, fmt.Errorf("%s job %s: missing budget evidence", path, name)
			}
			if err := checkJob(root, job, entry); err != nil {
				return checked, fmt.Errorf("%s job %s: %w", path, name, err)
			}
			delete(entries, name)
			checked++
		}
		if len(entries) != 0 {
			return checked, fmt.Errorf("%s: budget evidence names nonexistent jobs", name)
		}
		delete(recorded.Workflows, name)
	}
	if checked == 0 || len(recorded.Workflows) != 0 {
		return checked, fmt.Errorf("no workflow jobs found, or evidence names nonexistent workflows")
	}
	return checked, nil
}

func main() {
	checked, err := check(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ci-job-budgets:", err)
		os.Exit(1)
	}
	fmt.Printf("ci-job-budgets: %d jobs have measured timeout budgets\n", checked)
}
