// A deliberately small module with real branches for Stryker to mutate.
//
// It exists to prove the mutation pipeline agrees end to end — the adapter's
// argv, the config it writes, the report path it reads back and the tool's
// output format — not to demonstrate coverage breadth. What matters is that
// some mutants are KILLED by the tests next door and at least one is not, so a
// report of either shape says something about the pipeline rather than about
// this file's quality.

export function tally(values: number[]): number {
  let total = 0;
  for (const v of values) {
    if (v > 0) {
      total += v;
    }
  }
  return total;
}

export function classify(total: number): string {
  if (total === 0) {
    return "empty";
  }
  if (total < 10) {
    return "small";
  }
  return "large";
}

// Deliberately uncovered by the test file: one surviving mutant proves the
// report distinguishes killed from survived rather than reporting everything
// as killed.
export function describe(total: number): string {
  return total < 0 ? "negative" : "non-negative";
}
