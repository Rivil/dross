package mutation

// astspanScript is the span dumper Stryker's Constructs feeds to `node -` on
// stdin, behind a one-line prelude that defines __dross = {file, source}.
//
// It is JavaScript carried as a Go string rather than an embedded .js file
// on purpose: the binary stays single-file either way, but a .js file in the
// tree would surface JavaScript as one of dross's own languages to every
// scanner that walks the repo (TestDetectLanguagesDrossRootIsGoOnly), and
// the shared skip set exists for fixtures, not for shipped assets.
//
// It is deliberately a DUMB span dumper: every node directly under
// Program.body, with its first and last line, its kind and its identifier.
// Which spans a changed hunk maps to is decided in Go (expandToConstructs),
// where it is unit-tested without node.
//
// The parser is @babel/parser resolved out of Stryker's OWN dependency tree —
// the instrumenter that places Stryker's mutants parses with it, so the
// spans here are the spans the mutants are located by. Resolution hops
// core → instrumenter → @babel/parser through createRequire so a pnpm layout
// (nothing hoisted) resolves the same as npm's.
//
// Output: one JSON object on stdout. {"constructs":[{start,end,kind,name}]}
// on success, {"error":{"message","line","column"}} with exit 2 otherwise.
// Nothing else is written to stdout; diagnostics go to stderr. The script
// uses no template literals, so it can live inside this raw string.
const astspanScript = `'use strict';
// astspan.js — dump the top-level constructs of one TypeScript/JavaScript
// file as line spans. See astspan.go for why and how it is run.

const { createRequire } = require('module');
const path = require('path');

function fail(message, loc) {
  const error = { message: String(message) };
  if (loc && Number.isInteger(loc.line)) {
    error.line = loc.line;
    error.column = Number.isInteger(loc.column) ? loc.column : 0;
  }
  process.stdout.write(JSON.stringify({ error }) + '\n');
  process.exit(2);
}

function resolveParser(cwd) {
  let req = createRequire(path.join(cwd, 'package.json'));
  for (const hop of ['@stryker-mutator/core', '@stryker-mutator/instrumenter']) {
    try {
      req = createRequire(req.resolve(hop));
    } catch (e) {
      // A hop that does not resolve is not fatal by itself: a flat install
      // may still resolve the parser from where we already stand.
      break;
    }
  }
  try {
    return req('@babel/parser');
  } catch (e) {
    return null;
  }
}

function pluginsFor(file) {
  const ext = path.extname(file).toLowerCase();
  // .ts never gets jsx: <T> generics and angle-bracket assertions are
  // ambiguous with JSX, and a .ts file cannot contain JSX anyway.
  if (ext === '.tsx' || ext === '.jsx') {
    return ['typescript', 'jsx'];
  }
  return ['typescript'];
}

function nameOf(node) {
  if (!node) return '';
  if (node.id && node.id.type === 'Identifier') return node.id.name;
  if (node.type === 'VariableDeclaration' && node.declarations.length > 0) {
    const first = node.declarations[0].id;
    if (first && first.type === 'Identifier') return first.name;
  }
  return '';
}

function construct(node) {
  // The span is the OUTERMOST node — the export wrapper included, since a
  // mutant is kept only when its whole location lies inside the range. The
  // kind and name come from the declaration inside it, which is what a
  // reader recognises.
  let inner = node;
  if ((node.type === 'ExportNamedDeclaration' || node.type === 'ExportDefaultDeclaration')
      && node.declaration && /Declaration$/.test(node.declaration.type)) {
    inner = node.declaration;
  }
  return {
    start: node.loc.start.line,
    end: node.loc.end.line,
    kind: inner.type,
    name: nameOf(inner),
  };
}

function main() {
  if (typeof __dross !== 'object' || __dross === null || typeof __dross.source !== 'string') {
    fail('no request on stdin: expected a __dross prelude with file and source');
  }
  const cwd = process.cwd();
  const parser = resolveParser(cwd);
  if (!parser) {
    fail('cannot resolve @babel/parser from ' + cwd);
  }
  let ast;
  try {
    ast = parser.parse(__dross.source, {
      sourceType: 'module',
      plugins: pluginsFor(__dross.file),
      errorRecovery: false,
      ranges: false,
      tokens: false,
    });
  } catch (e) {
    fail(e && e.message ? e.message : e, e && e.loc);
  }
  const constructs = ast.program.body.map(construct);
  process.stdout.write(JSON.stringify({ constructs }) + '\n');
}

main();
`
