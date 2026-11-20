package main

import (
	"strings"
	"testing"
)

const goodWorkflow = `
jobs:
  changes-gate:
    runs-on: antimatter-vps-gate
    timeout-minutes: 1
    outputs: { runner: "${{ steps.pick.outputs.runner }}" }
    steps: [{ id: pick, run: runner-gate pick linux-x64 }]
  changes:
    needs: changes-gate
    runs-on: ${{ needs.changes-gate.outputs.runner }}
  test-macos-gate:
    needs: changes
    if: needs.changes.outputs.code == 'true'
    runs-on: antimatter-vps-gate
    timeout-minutes: 1
    outputs: { runner: "${{ steps.pick.outputs.runner }}" }
    steps: [{ id: pick, run: runner-gate pick macos-arm64 }]
  test-macos:
    needs: [changes, test-macos-gate]
    if: ${{ needs.changes.outputs.code == 'true' }}
    runs-on: ${{ needs.test-macos-gate.outputs.runner }}
  ci-ok-gate:
    if: always()
    needs: [changes, test-macos]
    runs-on: antimatter-vps-gate
    timeout-minutes: 1
    outputs: { runner: "${{ steps.pick.outputs.runner }}" }
    steps: [{ id: pick, run: runner-gate pick linux-x64 }]
  ci-ok:
    if: always()
    needs: [changes, test-macos, ci-ok-gate]
    runs-on: ${{ needs.ci-ok-gate.outputs.runner || 'ubuntu-24.04' }}
  reap:
    uses: owner/repo/.github/workflows/reap.yml@0123456789012345678901234567890123456789
`

func TestGoodWorkflow(t *testing.T) {
	if fails := checkWorkflow("ci.yml", []byte(goodWorkflow)); len(fails) != 0 {
		t.Fatalf("a workflow in the gate's shape was refused:\n%s", strings.Join(fails, "\n"))
	}
}

func TestRefusals(t *testing.T) {
	for _, test := range []struct {
		name, old, new, want string
	}{
		{"missing gate", "  changes-gate:\n    runs-on", "  other-gate:\n    runs-on", "job changes has no gate job changes-gate"},
		{"orphan gate", "  changes-gate:\n    runs-on", "  changes-gate:\n    runs-on", ""},
		{"job does not need its gate", "    needs: changes-gate\n", "", "does not need its gate changes-gate"},
		{"gate needs differ", "  test-macos-gate:\n    needs: changes\n", "  test-macos-gate:\n", "not the same needs as test-macos"},
		{"gate if differs", "  test-macos-gate:\n    needs: changes\n    if: needs.changes.outputs.code == 'true'\n", "  test-macos-gate:\n    needs: changes\n", "not the same condition as test-macos"},
		{"gate on a hosted runner", "  changes-gate:\n    runs-on: antimatter-vps-gate", "  changes-gate:\n    runs-on: ubuntu-24.04", "gate changes-gate must run on antimatter-vps-gate"},
		{"gate timeout", "  changes-gate:\n    runs-on: antimatter-vps-gate\n    timeout-minutes: 1", "  changes-gate:\n    runs-on: antimatter-vps-gate\n    timeout-minutes: 5", "timeout-minutes is 5, not 1"},
		{"gate output", "  changes-gate:\n    runs-on: antimatter-vps-gate\n    timeout-minutes: 1\n    outputs: { runner: \"${{ steps.pick.outputs.runner }}\" }", "  changes-gate:\n    runs-on: antimatter-vps-gate\n    timeout-minutes: 1\n    outputs: { label: \"${{ steps.pick.outputs.runner }}\" }", "outputs must be exactly runner"},
		{"gate step", "run: runner-gate pick linux-x64 }]\n  changes:", "run: curl gate }]\n  changes:", "step must be exactly"},
		{"unknown class", "run: runner-gate pick linux-x64 }]\n  changes:", "run: runner-gate pick linux-x86 }]\n  changes:", "unknown class"},
		{"macOS job on Linux", "pick macos-arm64", "pick linux-x64", "test-macos needs macos-arm64"},
		{"job ignores its gate", "    runs-on: ${{ needs.changes-gate.outputs.runner }}", "    runs-on: ubuntu-24.04", "job changes runs-on must be"},
		{"fallback on a job that is not always()", "    runs-on: ${{ needs.changes-gate.outputs.runner }}", "    runs-on: ${{ needs.changes-gate.outputs.runner || 'ubuntu-24.04' }}", "job changes runs-on must be"},
		{"always() job without fallback", "    runs-on: ${{ needs.ci-ok-gate.outputs.runner || 'ubuntu-24.04' }}", "    runs-on: ${{ needs.ci-ok-gate.outputs.runner }}", "runs if: always()"},
		{"legacy picker", "  reap:\n", "  runner:\n    uses: ./.github/workflows/runner-pick.yml\n  reap:\n", "runner-pick.yml"},
		{"legacy runners output", "    runs-on: ${{ needs.changes-gate.outputs.runner }}", "    runs-on: ${{ fromJSON(needs.x.outputs.runners)['changes'] }}", "outputs.runners"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !strings.Contains(goodWorkflow, test.old) {
				t.Fatalf("fixture does not contain %q", test.old)
			}
			content := strings.Replace(goodWorkflow, test.old, test.new, 1)
			if test.name == "orphan gate" {
				content += "  lonely-gate:\n    runs-on: antimatter-vps-gate\n"
				test.want = "gate lonely-gate serves no job"
			}
			fails := strings.Join(checkWorkflow("ci.yml", []byte(content)), "\n")
			if !strings.Contains(fails, test.want) {
				t.Fatalf("want a refusal containing %q, got:\n%s", test.want, fails)
			}
		})
	}
}

func TestMatrixOverOS(t *testing.T) {
	content := strings.Replace(goodWorkflow,
		"    runs-on: ${{ needs.changes-gate.outputs.runner }}\n",
		"    runs-on: ${{ needs.changes-gate.outputs.runner }}\n    strategy:\n      matrix:\n        os: [ubuntu-latest, macos-latest]\n", 1)
	fails := strings.Join(checkWorkflow("ci.yml", []byte(content)), "\n")
	if !strings.Contains(fails, "matrix over os") {
		t.Fatalf("a matrix over os was accepted:\n%s", fails)
	}
}

// The repository's own workflows, as they are: the check main runs.
func TestRepositoryWorkflows(t *testing.T) {
	checked, fails := check("../..")
	if checked == 0 || len(fails) != 0 {
		t.Fatalf("%d workflows checked:\n%s", checked, strings.Join(fails, "\n"))
	}
}
