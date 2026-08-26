import { expect, it } from "vitest";
import { pageSize, slugify } from "./handler";

it("slugifies an id", () => {
  expect(slugify("Beef Stew")).toBe("beef-stew");
});

it("names an empty id", () => {
  expect(slugify("")).toBe("unknown");
});

it("floors an under-range page size", () => {
  expect(pageSize(0)).toBe(20);
});

it("caps an over-range page size", () => {
  expect(pageSize(500)).toBe(100);
});

it("passes an in-range page size through", () => {
  expect(pageSize(42)).toBe(42);
});
