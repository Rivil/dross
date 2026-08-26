// A module whose PATH is the point.
//
// SvelteKit names dynamic route segments with square brackets, so a real
// --mutate list routinely carries paths like
// src/routes/recipes/[id]/+page.server.ts. Handed to Stryker unescaped, the
// tool's FileMatcher reads "[id]" as a glob character class — one of i, d, i —
// so the pattern matches nothing, the file is silently dropped from the run,
// and the report still comes back looking complete.
//
// The contents below only have to be mutatable and covered; what this fixture
// proves lives entirely in the directory name.

export function slugify(id: string): string {
  if (id === "") {
    return "unknown";
  }
  return id.toLowerCase().replace(/\s+/g, "-");
}

export function pageSize(requested: number): number {
  if (requested < 1) {
    return 20;
  }
  if (requested > 100) {
    return 100;
  }
  return requested;
}
