// A deliberately LONG function, for the construct-range proof.
//
// The pad heuristic this fixture retires widened a changed line by 25 lines
// each way. A mutant is kept only when its whole span lies inside the range,
// so the function body's own block mutant — whose span opens on the line
// below — was lost for any edit more than 25 lines into the body. The edit
// this fixture stands in for is the marked deep-edit line, well past that
// window; the construct span recovers what the window could not.
export function big(values: number[]): number {
  let total = 0;
  let positives = 0;
  let negatives = 0;
  let zeros = 0;
  for (const v of values) {
    if (v > 0) {
      positives += 1;
    } else if (v < 0) {
      negatives += 1;
    } else {
      zeros += 1;
    }
  }
  if (positives > negatives) {
    total += positives;
  }
  if (negatives > positives) {
    total -= negatives;
  }
  if (zeros > 0) {
    total += zeros;
  }
  let running = 0;
  for (const v of values) {
    if (v > 100) {
      running += 100;
    } else {
      running += v;
    }
  }
  if (running > total) {
    total = running;
  }
  if (total > 1000) {
    total = 1000; // dross:deep-edit
  }
  if (total < -1000) {
    total = -1000;
  }
  return total;
}

export function small<T>(xs: T[]): T {
  return xs[xs.length - 1];
}
