package codex

// This file defines the per-language AstGrepIndexer instances.
// Adding a new language is a one-function addition: pick the
// ast-grep language id, list the file extensions, and write
// patterns for each kind of symbol you want surfaced.
//
// Patterns use ast-grep's metavariable syntax:
//   $NAME    — captures a single token (the symbol identifier)
//   $_       — single-token wildcard (don't capture)
//   $$$      — multi-token wildcard (don't capture)
//
// Keep patterns specific enough to avoid false positives but loose
// enough to handle real-world variations. Test on a representative
// fixture file before committing a new pattern.

// TypeScriptIndexer covers .ts and .tsx via ast-grep's "ts" / "tsx"
// languages. We register two adapters (one per extension) so files
// with .tsx route through tsx (which understands JSX) and .ts files
// stay in the cheaper ts mode.
func TypeScriptIndexer() *AstGrepIndexer {
	return &AstGrepIndexer{
		LangName: "ts",
		Exts:     []string{".ts"},
		Patterns: tsPatterns(),
	}
}

func TSXIndexer() *AstGrepIndexer {
	return &AstGrepIndexer{
		LangName: "tsx",
		Exts:     []string{".tsx"},
		Patterns: tsPatterns(),
	}
}

func tsPatterns() []AstGrepPattern {
	return []AstGrepPattern{
		// function f(...) {...}
		{Kind: "function", Pattern: "function $NAME($$$ARGS) { $$$ }"},
		// export function f(...) {...}
		{Kind: "function", Pattern: "export function $NAME($$$ARGS) { $$$ }"},
		// export async function f(...) {...}
		{Kind: "function", Pattern: "export async function $NAME($$$ARGS) { $$$ }"},
		// async function f(...) {...}
		{Kind: "function", Pattern: "async function $NAME($$$ARGS) { $$$ }"},
		// const f = (...) => ...
		{Kind: "function", Pattern: "const $NAME = ($$$) => $$$"},
		{Kind: "function", Pattern: "export const $NAME = ($$$) => $$$"},
		// class C {}, export class C {}
		{Kind: "class", Pattern: "class $NAME { $$$ }"},
		{Kind: "class", Pattern: "export class $NAME { $$$ }"},
		// interface I {}
		{Kind: "interface", Pattern: "interface $NAME { $$$ }"},
		{Kind: "interface", Pattern: "export interface $NAME { $$$ }"},
		// type T = ...
		{Kind: "type", Pattern: "type $NAME = $$$"},
		{Kind: "type", Pattern: "export type $NAME = $$$"},
		// enum E {}
		{Kind: "enum", Pattern: "enum $NAME { $$$ }"},
		{Kind: "enum", Pattern: "export enum $NAME { $$$ }"},
	}
}

// SvelteIndexer handles .svelte files. ast-grep's "svelte" language
// understands the hybrid HTML+TS structure so the same TS patterns
// match symbols inside <script lang="ts"> blocks.
//
// Note: ast-grep's Svelte support is community-maintained and may
// lag the latest Svelte syntax (e.g. runes). Patterns here cover the
// stable surface; bleeding-edge Svelte 5 syntax may need revisiting.
func SvelteIndexer() *AstGrepIndexer {
	return &AstGrepIndexer{
		LangName: "svelte",
		Exts:     []string{".svelte"},
		Patterns: tsPatterns(),
	}
}

// CSharpIndexer covers .cs via ast-grep's "csharp" language.
func CSharpIndexer() *AstGrepIndexer {
	return &AstGrepIndexer{
		LangName: "csharp",
		Exts:     []string{".cs"},
		Patterns: []AstGrepPattern{
			{Kind: "class", Pattern: "class $NAME { $$$ }"},
			{Kind: "class", Pattern: "public class $NAME { $$$ }"},
			{Kind: "class", Pattern: "internal class $NAME { $$$ }"},
			{Kind: "interface", Pattern: "interface $NAME { $$$ }"},
			{Kind: "interface", Pattern: "public interface $NAME { $$$ }"},
			{Kind: "struct", Pattern: "struct $NAME { $$$ }"},
			{Kind: "struct", Pattern: "public struct $NAME { $$$ }"},
			{Kind: "enum", Pattern: "enum $NAME { $$$ }"},
			{Kind: "enum", Pattern: "public enum $NAME { $$$ }"},
			// Methods are tricky in C# because of access modifiers,
			// generics, async, etc. The naive pattern below catches
			// public/private methods with simple signatures; misses
			// generic methods. Refining is a follow-up.
			{Kind: "method", Pattern: "public $RET $NAME($$$) { $$$ }"},
			{Kind: "method", Pattern: "private $RET $NAME($$$) { $$$ }"},
		},
	}
}

// GDScriptIndexer covers .gd via ast-grep's "gdscript" language.
// ast-grep ships GDScript support but coverage of newer Godot 4
// syntax (typed signals, lambdas) varies — patterns here cover
// classic GDScript surface area.
func GDScriptIndexer() *AstGrepIndexer {
	return &AstGrepIndexer{
		LangName: "gdscript",
		Exts:     []string{".gd"},
		Patterns: []AstGrepPattern{
			{Kind: "function", Pattern: "func $NAME($$$): $$$"},
			{Kind: "function", Pattern: "func $NAME($$$):"},
			// `class_name Foo` declares the script's class identifier
			{Kind: "class", Pattern: "class_name $NAME"},
			// inner classes
			{Kind: "class", Pattern: "class $NAME: $$$"},
		},
	}
}

// allIndexers returns every registered Indexer. The Go indexer leads
// (stdlib parser, no shell-out) so .go files always route there
// regardless of ast-grep availability. AstGrep-backed indexers are
// safe to register unconditionally because Symbols() returns nil
// when the binary is missing.
func allIndexers() []Indexer {
	return []Indexer{
		&GoIndexer{},
		TypeScriptIndexer(),
		TSXIndexer(),
		SvelteIndexer(),
		CSharpIndexer(),
		GDScriptIndexer(),
	}
}
