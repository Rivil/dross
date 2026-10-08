package cmd

import (
	"fmt"
	"testing"
)

// The taint engine's scheduler (c-8).
//
// The engine used to reach its fixpoint in whole-program rounds: analyse every
// function, and go round again if anything anywhere grew. The live exec scan
// with markers ignored took 14 rounds — 35,518 function analyses. It now keeps
// a worklist, and a function is analysed again only when something it reads
// has grown: a callee's summary, a field it reads, a closure write-back at a
// MakeClosure it holds, or an allocation, captured variable or parameter seed
// it owns.
//
// Every input only grows, so any schedule that starts from nothing, adds only
// derived facts and stops at a fixpoint stops at the SAME one — the least. What
// can go wrong is stopping early: a missed dependency is an input that grew
// after its reader's last analysis. TestWorklistReachesTheFixpoint catches
// exactly that, by re-analysing every function once more and requiring nothing
// to grow. (When the scheduler changed, both were also run side by side on the
// live tree, the live path scan and every corpus: byte-identical findings and
// exact origins.)

// taintAnalysisBudgetPerFunc bounds the analyses the live exec scan may make
// with every marker ignored, per function in the program. A worklist
// re-analyses a function only when one of its inputs grew; the live tree
// measured ~2.27 analyses per function at 2,537 functions and 2.41 at 2,998
// (2026-10-04), so a fixed count fell behind the tree's growth. Whole-program
// rounds need at least three passes, so a ratio under 3 tells a worklist from
// rounds at any size.
const taintAnalysisBudgetPerFunc = 2.75

// taintAnalysisBudget is the analysis budget for a program of funcs functions.
func taintAnalysisBudget(funcs int) int { return int(taintAnalysisBudgetPerFunc * float64(funcs)) }

func taintAnalysisBudgetErr(n, funcs int) error {
	if budget := taintAnalysisBudget(funcs); n > budget {
		return fmt.Errorf("the live exec scan made %d function analyses, over its budget of %d (%.2f per function over %d) — "+
			"the scheduler is re-analysing functions whose inputs did not grow", n, budget, taintAnalysisBudgetPerFunc, funcs)
	}
	return nil
}

// TestWorklistReachesTheFixpoint: on the live tree with every marker ignored —
// the most taint it carries, through every flow kind — one more pass over
// every function once the worklist empties grows nothing, and the run stays
// within its analysis budget.
func TestWorklistReachesTheFixpoint(t *testing.T) {
	fs, _, st := runTaintWith(liveView(t), execTaintPolicy(), taintRunOpts{verifyFixpoint: true})
	if len(fs) == 0 {
		t.Error("the live exec scan with markers ignored found nothing — the fixpoint check is vacuous")
	}
	if !st.FixpointHeld {
		t.Error("re-analysing every function after the worklist emptied grew a summary, shared state or a finding — " +
			"a dependency is missing from the scheduler, and it stopped before the fixpoint")
	}
	if err := taintAnalysisBudgetErr(st.Analyses, st.Funcs); err != nil {
		t.Error(err)
	}
	// A scheduler back on whole-program rounds needs at least three passes
	// here (the live tree took 14); the budget must not admit three.
	if taintAnalysisBudget(st.Funcs) >= 3*st.Funcs {
		t.Errorf("the budget (%d) admits three whole-program passes over %d functions — it cannot tell a worklist from rounds",
			taintAnalysisBudget(st.Funcs), st.Funcs)
	}
	t.Logf("worklist: %d analyses over %d functions, %d findings", st.Analyses, st.Funcs, len(fs))
}

// TestTaintAnalysisBudget: the budget passes at its value and fails one over.
func TestTaintAnalysisBudget(t *testing.T) {
	const funcs = 3000
	if taintAnalysisBudgetErr(taintAnalysisBudget(funcs), funcs) != nil || taintAnalysisBudgetErr(taintAnalysisBudget(funcs)+1, funcs) == nil {
		t.Error("the budget must pass at its value and fail one over")
	}
}
