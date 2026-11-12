import { render } from "svelte/server";
import { expect, it } from "vitest";
import Widget from "./Widget.svelte";

// The import is what relates this test to Widget.svelte, which a Stryker leg on
// that file needs to collect any test at all. A server render runs the markup
// and never a click handler, so the template's mutants can die here while
// increment's and reset's stay NoCoverage. The ranging proof needs both kinds.
it("renders the count plus one", () => {
  expect(render(Widget).body).toMatch(/>\s*1\s*</);
});

it("renders the few branch for a low count", () => {
  const { body } = render(Widget);
  expect(body).toContain("few");
  expect(body).not.toContain("many");
});
