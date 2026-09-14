package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// No `${{ }}` expression may appear inside a `run:` block in any workflow. An
// inline expression is substituted into the script text before the shell sees
// it, so whatever it expands to is parsed as code — the classic injection
// vector for untrusted contexts (issue titles, PR bodies, inputs). The rule here
// is blanket, not trust-scoped (locked decision run_block_expression_rule): a
// blanket rule is testable without a trust taxonomy, and values reach the shell
// through `env:` just as easily. Phase supply-chain-currency, criterion c-5.

// runBlockExpression is one `${{` found inside a run: block.
type runBlockExpression struct {
	line int
	text string
}

// runBlockExpressions walks a workflow by line and reports every line inside a
// `run:` value that contains `${{`. Line-based on purpose (the repo carries no
// YAML dependency). A single-line `run: <cmd>` is its own block; `run: |` /
// `run: >` (with optional chomping indicator) opens a block that continues
// through every following line that is blank or indented deeper than the `run:`
// key. Expressions in `env:`, `with:`, `if:` or anywhere else are not run-block
// content and are never reported.
func runBlockExpressions(workflow string) []runBlockExpression {
	var (
		found    []runBlockExpression
		inBlock  bool
		runDepth int
	)
	for i, raw := range strings.Split(workflow, "\n") {
		trimmed := strings.TrimSpace(raw)
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if inBlock {
			if trimmed != "" && indent <= runDepth {
				inBlock = false
			} else {
				if strings.Contains(raw, "${{") {
					found = append(found, runBlockExpression{line: i + 1, text: trimmed})
				}
				continue
			}
		}
		key := strings.TrimPrefix(trimmed, "- ")
		rest, ok := strings.CutPrefix(key, "run:")
		if !ok {
			continue
		}
		value := strings.TrimSpace(rest)
		if isBlockScalarIndicator(value) {
			inBlock = true
			// A `- run: |` list item's continuation lines are indented past
			// the dash, so the key depth is where `run:` itself starts.
			runDepth = indent
			if strings.HasPrefix(trimmed, "- ") {
				runDepth += 2
			}
			continue
		}
		if strings.Contains(value, "${{") {
			found = append(found, runBlockExpression{line: i + 1, text: trimmed})
		}
	}
	return found
}

// isBlockScalarIndicator reports whether a scalar value opens a YAML block
// scalar: `|` or `>` followed by optional chomping/indentation indicators.
func isBlockScalarIndicator(value string) bool {
	if value == "" {
		return false
	}
	if value[0] != '|' && value[0] != '>' {
		return false
	}
	return strings.Trim(value[1:], "+-0123456789") == ""
}

func TestRunBlockScanner(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want []int // 1-based lines that must be reported, in order
	}{
		{
			name: "single-line run with an expression",
			yaml: "steps:\n  - run: echo ${{ github.event.inputs.x }}\n",
			want: []int{2},
		},
		{
			name: "single-line run, key after name",
			yaml: "steps:\n  - name: x\n    run: echo ${{ x }}\n",
			want: []int{3},
		},
		{
			name: "literal block: expression on a middle line",
			yaml: "steps:\n  - name: x\n    run: |\n      set -e\n      v=\"${{ steps.tag.outputs.new_tag }}\"\n      echo \"$v\"\n",
			want: []int{5},
		},
		{
			name: "folded block with chomping indicator",
			yaml: "steps:\n  - run: >-\n      echo\n      ${{ x }}\n",
			want: []int{4},
		},
		{
			name: "block ends at the next key; expression after it is env, not run",
			yaml: "steps:\n  - name: x\n    run: |\n      echo hi\n    env:\n      TOKEN: ${{ secrets.TOKEN }}\n",
			want: nil,
		},
		{
			name: "block followed by a sibling step whose run is clean",
			yaml: "steps:\n  - run: |\n      echo one\n\n      echo two\n  - run: echo three\n  - run: echo ${{ x }}\n",
			want: []int{7},
		},
		{
			name: "env: and with: values are never reported (false-positive guard)",
			yaml: "steps:\n  - uses: a/b@sha\n    with:\n      token: ${{ secrets.T }}\n    env:\n      A: ${{ runner.temp }}/x\n  - name: y\n    if: ${{ github.ref == 'main' }}\n    env:\n      B: ${{ steps.tag.outputs.new_tag }}\n    run: echo \"$B\"\n",
			want: nil,
		},
		{
			name: "multiple expressions in one block are each reported",
			yaml: "steps:\n  - run: |\n      a=${{ x }}\n      b=ok\n      c=${{ y }}\n",
			want: []int{3, 5},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runBlockExpressions(tc.yaml)
			var lines []int
			for _, g := range got {
				lines = append(lines, g.line)
			}
			if fmt.Sprint(lines) != fmt.Sprint(tc.want) {
				t.Errorf("reported lines %v, want %v (hits: %+v)", lines, tc.want, got)
			}
		})
	}
}

// TestWorkflowsHaveNoExpressionsInRun sweeps every workflow file. Values reach
// a run: script via env: — never inline.
func TestWorkflowsHaveNoExpressionsInRun(t *testing.T) {
	root := repoRootFromTest(t)
	workflows, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) == 0 {
		t.Fatal("no .github/workflows/*.yml found — nothing swept")
	}
	for _, wf := range workflows {
		rel, _ := filepath.Rel(root, wf)
		for _, hit := range runBlockExpressions(readRepoFile(t, rel)) {
			t.Errorf("%s:%d `${{ }}` inside a run: block — pass it through env: instead: %s", rel, hit.line, hit.text)
		}
	}
}
