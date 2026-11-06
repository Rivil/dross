import { compile } from "svelte/compiler";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [
    {
      // Vitest transforms every import to find the tests related to a mutated
      // file, so without this one test importing a .svelte fails every Stryker
      // run here. Server output needs no DOM, and the svelte pin is already this
      // fixture's: a whole plugin package would buy nothing a test here uses.
      name: "dross-fixture-svelte",
      enforce: "pre",
      transform(code, id) {
        if (!id.endsWith(".svelte")) {
          return null;
        }
        const { js } = compile(code, { filename: id, generate: "server" });
        return { code: js.code, map: js.map };
      },
    },
  ],
  test: {
    // Explicit include: Stryker's vitest runner re-runs this config per mutant,
    // and a default glob that reached outside the fixture would make the run
    // depend on whatever happens to sit above it in the repo.
    include: ["src/**/*.test.ts"],
    environment: "node",
  },
});
