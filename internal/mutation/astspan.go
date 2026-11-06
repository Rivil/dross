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
// where it is unit-tested without node. A .svelte file dumps the same way,
// and its same-line collisions are merged in Go too (mergeConstructs).
//
// The parser is @babel/parser resolved out of Stryker's OWN dependency tree —
// the instrumenter that places Stryker's mutants parses with it, so the
// spans here are the spans the mutants are located by. Resolution hops
// core → instrumenter → @babel/parser through createRequire so a pnpm layout
// (nothing hoisted) resolves the same as npm's.
//
// A .svelte file has no single Program, so its spans come in two kinds. Each
// <script> body goes to the same Babel, told its starting line so its spans
// are file lines, not block lines; each top-level markup node is one span,
// because Stryker places mutants in template expressions too. The block
// offsets and the markup nodes come from the project's own svelte/compiler
// (legacy AST, which is what Stryker reads across Svelte 3-5), never a regex
// that would find a <script> inside a comment.
//
// Scripts and styles are blanked to same-length whitespace before the compiler
// sees them: it cannot parse lang="ts" before Svelte 5 nor lang="scss" at all,
// and blanking keeps every offset it reports valid against the real source.
//
// Output: one JSON object on stdout. {"constructs":[{start,end,kind,name}]}
// on success, {"error":{"message","line","column"}} with exit 2 otherwise.
// Nothing else is written to stdout; diagnostics go to stderr. The script
// uses no template literals, so it can live inside this raw string.
const astspanScript = `'use strict';
// astspan.js — dump the top-level constructs of one TypeScript, JavaScript or
// Svelte file as line spans. See astspan.go for why and how it is run.

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

function requireParser(cwd) {
  const parser = resolveParser(cwd);
  if (!parser) {
    fail('cannot resolve @babel/parser from ' + cwd);
  }
  return parser;
}

function resolveCompiler(cwd) {
  try {
    return createRequire(path.join(cwd, 'package.json'))('svelte/compiler');
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
  // A .svelte script lands here whatever its lang: plain JS parses as
  // TypeScript, and only the spans are wanted.
  return ['typescript'];
}

function babelParse(parser, source, position) {
  try {
    return parser.parse(source, Object.assign({
      sourceType: 'module',
      plugins: pluginsFor(__dross.file),
      errorRecovery: false,
      ranges: false,
      tokens: false,
    }, position));
  } catch (e) {
    fail(e && e.message ? e.message : e, e && e.loc);
  }
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

// locate turns a source offset into the 1-based line and 0-based column Babel
// and the compiler both report positions in.
function locate(source, offset) {
  const before = source.slice(0, offset);
  return {
    line: before.split('\n').length,
    column: offset - (before.lastIndexOf('\n') + 1),
  };
}

function blank(block) {
  return { code: block.content.replace(/[^\n]/g, ' ') };
}

function markupConstruct(source, node) {
  return {
    start: locate(source, node.start).line,
    end: locate(source, node.end - 1).line,
    kind: node.type,
    name: typeof node.name === 'string' ? node.name : '',
  };
}

async function svelteConstructs(cwd) {
  const compiler = resolveCompiler(cwd);
  if (!compiler) {
    fail('cannot resolve svelte/compiler from ' + cwd);
  }
  const parser = requireParser(cwd);
  const source = __dross.source;
  let ast;
  try {
    const blanked = await compiler.preprocess(source, { script: blank, style: blank });
    ast = compiler.parse(blanked.code, { filename: __dross.file });
  } catch (e) {
    fail(e && e.message ? e.message : e, e && e.start);
  }
  const constructs = [];
  for (const block of [ast.module, ast.instance]) {
    if (!block) continue;
    const { start, end } = block.content;
    const at = locate(source, start);
    const script = babelParse(parser, source.slice(start, end), { startLine: at.line, startColumn: at.column });
    constructs.push(...script.program.body.map(construct));
  }
  // Text and comments hold no expression, so no mutant: a hunk on one stays a
  // bare hunk. A top-level <style> is not in html at all.
  for (const node of ast.html.children || []) {
    if (node.type !== 'Text' && node.type !== 'Comment') {
      constructs.push(markupConstruct(source, node));
    }
  }
  return constructs;
}

async function main() {
  if (typeof __dross !== 'object' || __dross === null || typeof __dross.source !== 'string') {
    fail('no request on stdin: expected a __dross prelude with file and source');
  }
  const cwd = process.cwd();
  const constructs = path.extname(__dross.file).toLowerCase() === '.svelte'
    ? await svelteConstructs(cwd)
    : babelParse(requireParser(cwd), __dross.source).program.body.map(construct);
  process.stdout.write(JSON.stringify({ constructs }) + '\n');
}

main().catch(function (e) {
  fail(e && e.message ? e.message : e);
});
`
