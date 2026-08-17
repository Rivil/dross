import { describe as group, expect, it } from "vitest";
import { classify, tally } from "./tally";

group("tally", () => {
  it("sums the positive values", () => {
    expect(tally([1, 2, 3])).toBe(6);
  });

  it("ignores non-positive values", () => {
    expect(tally([1, -5, 0, 2])).toBe(3);
  });

  it("is zero for an empty list", () => {
    expect(tally([])).toBe(0);
  });
});

group("classify", () => {
  it("names an empty total", () => {
    expect(classify(0)).toBe("empty");
  });

  it("names a small total", () => {
    expect(classify(9)).toBe("small");
  });

  it("names a large total", () => {
    expect(classify(10)).toBe("large");
  });
});
