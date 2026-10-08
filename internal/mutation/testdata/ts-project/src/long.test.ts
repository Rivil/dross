import { describe as group, expect, it } from "vitest";
import { big, small } from "./long";

group("big", () => {
  it("counts a positive-heavy list", () => {
    expect(big([1, 2, 3, -1])).toBe(5);
  });

  it("counts a negative-heavy list", () => {
    expect(big([-1, -2, 1])).toBe(-2);
  });

  it("counts zeros", () => {
    expect(big([0, 0, 1, -1])).toBe(2);
  });

  it("caps large values", () => {
    expect(big([500, 500, 500])).toBe(300);
  });

  it("clamps the total", () => {
    expect(big(Array(20).fill(100))).toBe(1000);
  });

  it("is zero for an empty list", () => {
    expect(big([])).toBe(0);
  });
});

group("small", () => {
  it("returns the last element", () => {
    expect(small([1, 2, 3])).toBe(3);
    expect(small(["a"])).toBe("a");
  });
});
