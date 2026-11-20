// ci-runner-gates checks that every job in .github/workflows takes its runner
// from its own gate job on the antimatter-vps runner gate, in the one shape the
// gate accepts:
//
//	<job>-gate:
//	  needs: [...]              # the same needs as <job>
//	  if: ...                   # the same condition as <job>, when it has one
//	  runs-on: antimatter-vps-gate
//	  timeout-minutes: 1
//	  outputs: { runner: "${{ steps.pick.outputs.runner }}" }
//	  steps: [{ id: pick, run: runner-gate pick <class> }]
//
//	<job>:
//	  needs: [..., <job>-gate]
//	  runs-on: ${{ needs.<job>-gate.outputs.runner }}
//
// A job with `if: always()` reads `${{ needs.<job>-gate.outputs.runner ||
// '<fallback>' }}` instead, because its gate can be skipped or fail with the
// rest of the run and an empty runs-on never starts. A job that calls a
// reusable workflow has no runs-on of its own and is left alone.
//
// It also refuses what the gate replaced: runner-pick.yml, the gate's HTTP
// endpoint, and the `runners` JSON the old picker handed out. A workflow that
// still asks the old way would hold a GitHub-hosted runner just to ask.
//
//	go run ./scripts/ci-runner-gates
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// gateLabel is the runner every gate job runs on.
const gateLabel = "antimatter-vps-gate"

// gateStep is the gate job's one step; the class is matched separately.
var gateStep = regexp.MustCompile(`^runner-gate pick ([a-z0-9-]+)$`)

// classes are the runner classes the gate answers for.
var classes = map[string]bool{
	"linux-x64": true, "linux-x64-kvm": true, "linux-arm64": true,
	"macos-arm64": true, "windows-x64": true, "windows-arm64": true,
}

// legacy is text that only the replaced picker used.
var legacy = []string{"runner-pick.yml", "runner-gate.antimatter-studios.com", "outputs.runners"}

type job struct {
	If       string            `yaml:"if"`
	Needs    yaml.Node         `yaml:"needs"`
	RunsOn   yaml.Node         `yaml:"runs-on"`
	Timeout  int               `yaml:"timeout-minutes"`
	Outputs  map[string]string `yaml:"outputs"`
	Steps    []map[string]any  `yaml:"steps"`
	Uses     string            `yaml:"uses"`
	Strategy struct {
		Matrix map[string]any `yaml:"matrix"`
	} `yaml:"strategy"`
}

type workflow struct {
	Jobs map[string]job `yaml:"jobs"`
}

func needsOf(node yaml.Node) []string {
	var list []string
	switch node.Kind {
	case yaml.ScalarNode:
		list = []string{node.Value}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			list = append(list, item.Value)
		}
	}
	sort.Strings(list)
	return list
}

// condition strips the optional ${{ }} around a job-level if.
func condition(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "${{") && strings.HasSuffix(text, "}}") {
		text = strings.TrimSpace(text[3 : len(text)-2])
	}
	return text
}

func isGate(name string) bool { return name == "gate" || strings.HasSuffix(name, "-gate") }

// expectedClass is the class this repository's jobs ask for: a job named for
// macOS runs on a Mac, every other job on Linux x64. Nothing here loads a
// kernel module, so nothing asks for KVM.
func expectedClass(name string) string {
	if strings.Contains(name, "macos") {
		return "macos-arm64"
	}
	return "linux-x64"
}

// checkWorkflow returns every way the workflow departs from the gate design.
func checkWorkflow(file string, content []byte) []string {
	var fails []string
	fail := func(format string, args ...any) {
		fails = append(fails, file+": "+fmt.Sprintf(format, args...))
	}
	for _, text := range legacy {
		if strings.Contains(string(content), text) {
			fail("still names %q, which only the replaced runner picker used", text)
		}
	}
	var parsed workflow
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		fail("%v", err)
		return fails
	}
	if len(parsed.Jobs) == 0 {
		fail("no jobs found")
		return fails
	}
	served := map[string]bool{}
	for name, j := range parsed.Jobs {
		if isGate(name) || j.Uses != "" {
			continue
		}
		gateName := name + "-gate"
		served[gateName] = true
		g, ok := parsed.Jobs[gateName]
		if !ok {
			fail("job %s has no gate job %s", name, gateName)
			continue
		}
		if len(j.Strategy.Matrix) != 0 {
			if _, hasOS := j.Strategy.Matrix["os"]; hasOS {
				fail("job %s is a matrix over os, but one gate answers one class: split it into a job per platform", name)
			}
		}
		needs := needsOf(j.Needs)
		hasGate := false
		var rest []string
		for _, need := range needs {
			if need == gateName {
				hasGate = true
			} else {
				rest = append(rest, need)
			}
		}
		if !hasGate {
			fail("job %s does not need its gate %s", name, gateName)
		}
		if got := needsOf(g.Needs); strings.Join(got, ",") != strings.Join(rest, ",") {
			fail("gate %s needs %v, not the same needs as %s (%v)", gateName, got, name, rest)
		}
		if condition(g.If) != condition(j.If) {
			fail("gate %s has if %q, not the same condition as %s (%q)", gateName, condition(g.If), name, condition(j.If))
		}
		want := "${{ needs." + gateName + ".outputs.runner }}"
		always := strings.Contains(condition(j.If), "always()")
		if always {
			fallback := regexp.MustCompile(`^\$\{\{ needs\.` + regexp.QuoteMeta(gateName) + `\.outputs\.runner \|\| '[a-z0-9.-]+' \}\}$`)
			if j.RunsOn.Kind != yaml.ScalarNode || !fallback.MatchString(j.RunsOn.Value) {
				fail("job %s runs if: always(), so its runs-on must be ${{ needs.%s.outputs.runner || '<fallback>' }}, not %q", name, gateName, j.RunsOn.Value)
			}
		} else if j.RunsOn.Kind != yaml.ScalarNode || j.RunsOn.Value != want {
			fail("job %s runs-on must be %s, not %q", name, want, j.RunsOn.Value)
		}
		if g.RunsOn.Kind != yaml.ScalarNode || g.RunsOn.Value != gateLabel {
			fail("gate %s must run on %s, not %q", gateName, gateLabel, g.RunsOn.Value)
		}
		if g.Timeout != 1 {
			fail("gate %s timeout-minutes is %d, not 1", gateName, g.Timeout)
		}
		if len(g.Outputs) != 1 || g.Outputs["runner"] != "${{ steps.pick.outputs.runner }}" {
			fail("gate %s outputs must be exactly runner: ${{ steps.pick.outputs.runner }}", gateName)
		}
		if len(g.Strategy.Matrix) != 0 {
			fail("gate %s must not be a matrix", gateName)
		}
		if len(g.Steps) != 1 {
			fail("gate %s has %d steps, not the one pick step", gateName, len(g.Steps))
			continue
		}
		step := g.Steps[0]
		run, _ := step["run"].(string)
		id, _ := step["id"].(string)
		match := gateStep.FindStringSubmatch(strings.TrimSpace(run))
		if id != "pick" || match == nil || len(step) != 2 {
			fail("gate %s step must be exactly { id: pick, run: runner-gate pick <class> }", gateName)
			continue
		}
		if !classes[match[1]] {
			fail("gate %s asks for unknown class %q", gateName, match[1])
		} else if want := expectedClass(name); match[1] != want {
			fail("gate %s asks for class %s, but %s needs %s", gateName, match[1], name, want)
		}
	}
	for name := range parsed.Jobs {
		if isGate(name) && !served[name] {
			fail("gate %s serves no job: a gate is named <job>-gate after the job it places", name)
		}
	}
	sort.Strings(fails)
	return fails
}

func check(root string) (int, []string) {
	var fails []string
	if _, err := os.Stat(filepath.Join(root, ".github", "workflows", "runner-pick.yml")); err == nil {
		fails = append(fails, ".github/workflows/runner-pick.yml: the replaced runner picker still exists")
	}
	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*"))
	if err != nil {
		return 0, append(fails, err.Error())
	}
	checked := 0
	for _, path := range paths {
		if !strings.HasSuffix(path, ".yml") && !strings.HasSuffix(path, ".yaml") {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			fails = append(fails, err.Error())
			continue
		}
		checked++
		fails = append(fails, checkWorkflow(filepath.Base(path), content)...)
	}
	if checked == 0 {
		fails = append(fails, "no workflows found")
	}
	return checked, fails
}

func main() {
	checked, fails := check(".")
	for _, f := range fails {
		fmt.Fprintln(os.Stderr, "ci-runner-gates:", f)
	}
	if len(fails) != 0 {
		os.Exit(1)
	}
	fmt.Printf("ci-runner-gates: %d workflows take every runner from a gate job\n", checked)
}
