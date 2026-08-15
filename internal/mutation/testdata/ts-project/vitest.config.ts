import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // Explicit include: Stryker's vitest runner re-runs this config per mutant,
    // and a default glob that reached outside the fixture would make the run
    // depend on whatever happens to sit above it in the repo.
    include: ["src/**/*.test.ts"],
    environment: "node",
  },
});
