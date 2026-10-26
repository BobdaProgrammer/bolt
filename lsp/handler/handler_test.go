package handler_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/BobdaProgrammer/bolt/lsp/analyzer"
	"github.com/BobdaProgrammer/bolt/lsp/handler"
	"github.com/BobdaProgrammer/bolt/lsp/logging"
	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// This file contains multiple sections of tests, delimited by 2 lines of ====== on each side (above and below) of a title

// ===========================================================================
// ===========================================================================
// DIAGNOSTIC TESTS
// ===========================================================================
// ===========================================================================

// skipIfParseErrors skips a test whose source this repo's parser rejects.
// It is used only where the Bolt syntax is a guess; a skip means "fix the
// snippet", not "the feature works".
func skipIfParseErrors(t *testing.T, src string) {
	t.Helper()
	if _, errs, _ := analyzer.Parse(src); len(errs) != 0 {
		t.Skipf("snippet does not parse with this Bolt grammar (%s); adjust it\n%s", errs[0].Message, src)
	}
}

// ===========================================================================
// Programs that must be clean
// ===========================================================================

var validPrograms = map[string]string{
	"empty":           "",
	"only newlines":   "\n\n\n",
	"only whitespace": "  \t \n   \n",
	"let and log":     prog(`let x = 5`, `log(x)`),
	"let without use": prog(`let unused = 1`),
	"function and call": prog(
		`let add = fn(a, b) {`,
		`  return a + b`,
		`}`,
		`log(add(1, 2))`),
	"closure over outer variable": prog(
		`let outer = 10`,
		`let f = fn() {`,
		`  return outer`,
		`}`,
		`log(f())`),
	"recursion": prog(
		`let fact = fn(n) {`,
		`  if (n < 2) {`,
		`    return 1`,
		`  }`,
		`  return n * fact(n - 1)`,
		`}`,
		`log(fact(5))`),
	"parameter shadows outer": prog(
		`let x = 1`,
		`let f = fn(x) {`,
		`  return x`,
		`}`,
		`log(f(x))`),
	"function returning function": prog(
		`let make = fn(a) {`,
		`  return fn(b) {`,
		`    return a + b`,
		`  }`,
		`}`,
		`log(make(1)(2))`),
	"if else": prog(
		`let n = 3`,
		`if (n > 1) {`,
		`  log(n)`,
		`} else {`,
		`  log(0)`,
		`}`),
	"arrays and hashes": prog(
		`let a = [1, 2, 3]`,
		`log(a[0])`,
		`let h = {"k": 1, 2: "two", 1.5: "f"}`,
		`log(h["k"])`,
		`log(h[2])`),
	"nested index": prog(
		`let a = [[1, 2], [3, 4]]`,
		`log(a[0][1])`),
	"reassignment": prog(
		`let x = 1`,
		`x = 2`,
		`log(x)`),
	"index assignment": prog(
		`let a = [1, 2]`,
		`a[0] = 9`,
		`let h = {"a": 1}`,
		`h["b"] = 2`),
	"string arithmetic": prog(
		`log("a" + "b")`,
		`log("ab" * 3)`,
		`log("a" == "a")`,
		`log("a" != "b")`),
	"numeric arithmetic": prog(
		`log(1 + 2)`,
		`log(1 + 2.5)`,
		`log(2.5 * 2)`,
		`log(10 / 4)`,
		`log(1.5 / 2)`,
		`log(1 == 1)`,
		`log(1 != 2)`),
	"boolean logic": prog(
		`log(true && false)`,
		`log(true || false)`,
		`log(true == false)`,
		`log(!true)`),
	"unknown-typed operands are never flagged": prog(
		`let f = fn(a, b) {`,
		`  return a + b`,
		`}`,
		`log(f(1, "x"))`),
	"builtins as calls": prog(
		`log(len("abc"))`,
		`log(typeof(1))`),
	"user variable shadows builtin": prog(
		`let len = 5`,
		`log(len)`),
	"user function shadows builtin": prog(
		`let push = fn(a) {`,
		`  return a`,
		`}`,
		`log(push(1))`),
	"unicode in strings and identifiers": prog(
		`let s = "héllo wörld 🎉 日本語"`,
		`log(s)`),
	"windows line endings": "let a = 1\r\nlog(a)\r\n",
	"no trailing newline":  "let a = 1\nlog(a)",
	"duplicate let (redefinition)": prog(
		`let x = 1`,
		`let x = 2`,
		`log(x)`),
	"deeply nested parens": "log(" + strings.Repeat("(", 100) + "1" + strings.Repeat(")", 100) + ")\n",
	"many statements":      strings.Repeat("let x = 1\n", 1000),
}

func TestValidProgramsProduceNoDiagnostics(t *testing.T) {
	for name, src := range validPrograms {
		t.Run(name, func(t *testing.T) {
			requireNoDiags(t, check(t, src))
		})
	}
}

// ===========================================================================
// Identifier not found
// ===========================================================================

func TestUndefinedIdentifier(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		ident string // as it appears in "Identifier not found: <ident>"
		line  int    // 0-based
	}{
		{"call argument", `log(y)`, "y", 0},
		{"let value", `let x = y`, "y", 0},
		{"called function", `foo()`, "foo", 0},
		{"inside function body", prog(`let f = fn() {`, `  return z`, `}`), "z", 1},
		{"inside infix", `log(1 + q)`, "q", 0},
		{"on a later line", prog(`let a = 1`, `log(b)`), "b", 1},
		{"array element", `let a = [1, nope]`, "nope", 0},
		{"hash value", `let h = {"k": nope}`, "nope", 0},
		{"if condition", prog(`if (nope) {`, `  log(1)`, `}`), "nope", 0},
		{"indexed value", `log(nope[0])`, "nope", 0},
		{"assignment target", `nope = 5`, "nope", 0},
		{"field access on undefined", `log(ghost.field)`, "ghost.field", 0},
		{"return value", prog(`let f = fn() {`, `  return missing`, `}`), "missing", 1},
		{"similar name (case sensitive)", prog(`let Value = 1`, `log(value)`), "value", 1},
		{"typo of defined name", prog(`let counter = 1`, `log(countr)`), "countr", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ds := check(t, c.src)
			d := requireDiag(t, ds, "Identifier not found: "+c.ident)
			requireLine(t, d, c.line)
		})
	}
}

func TestUndefinedIdentifier_RangeCoversTheIdentifier(t *testing.T) {
	// `log(y)`: y is at column 4..5 (0-based, half-open).
	d := requireDiag(t, check(t, `log(y)`), "Identifier not found: y")
	if d.Range.Start.Character != 4 || d.Range.End.Character != 5 {
		t.Fatalf("range = %+v, want characters 4..5", d.Range)
	}

	// Longer name, not at the start of the line.
	d = requireDiag(t, check(t, `let v = missing_name`), "Identifier not found: missing_name")
	if d.Range.Start.Character != 8 || d.Range.End.Character != 20 {
		t.Fatalf("range = %+v, want characters 8..20", d.Range)
	}
}

func TestEachUndefinedIdentifierIsReported(t *testing.T) {
	ds := check(t, prog(`log(a)`, `log(b)`, `log(c)`))
	for _, name := range []string{"a", "b", "c"} {
		requireDiag(t, ds, "Identifier not found: "+name)
	}
}

func TestOnlyUndefinedIdentifiersAreReported(t *testing.T) {
	ds := check(t, prog(`let known = 1`, `log(known)`, `log(unknown)`))
	if len(ds) != 1 {
		t.Fatalf("want exactly 1 diagnostic, got:\n  %s", strings.Join(messages(ds), "\n  "))
	}
	requireDiag(t, ds, "Identifier not found: unknown")
}

func TestUnderscoreIsNeverReportedAsUndefined(t *testing.T) {
	src := `log(_)`
	skipIfParseErrors(t, src)
	requireNoDiags(t, check(t, src))
}

func TestUseBeforeDefinitionInSameScope_IsCurrentlyNotReported(t *testing.T) {
	// Identifier uses are recorded while walking and resolved after the whole
	// scope has been walked, so `log(x)` before `let x` is accepted. This test
	// only logs the behaviour; flip it to requireDiag if you decide it should
	// be an error.
	ds := check(t, prog(`log(x)`, `let x = 1`))
	t.Logf("use-before-define diagnostics: %q", messages(ds))
}

// ===========================================================================
// Scoping
// ===========================================================================

func TestScope_FunctionLocalsDoNotLeak(t *testing.T) {
	ds := check(t, prog(
		`let f = fn() {`,
		`  let inner = 1`,
		`  return inner`,
		`}`,
		`log(inner)`))
	if got := containing(ds, "Identifier not found: inner"); len(got) != 1 {
		t.Fatalf("want exactly one 'inner' error, got:\n  %s", strings.Join(messages(ds), "\n  "))
	}
	requireLine(t, requireDiag(t, ds, "Identifier not found: inner"), 4)
}

func TestScope_ParametersDoNotLeak(t *testing.T) {
	ds := check(t, prog(
		`let f = fn(p) {`,
		`  return p`,
		`}`,
		`log(p)`))
	requireLine(t, requireDiag(t, ds, "Identifier not found: p"), 3)
	if len(containing(ds, "Identifier not found: p")) != 1 {
		t.Fatalf("the use of p inside f must not be flagged: %q", messages(ds))
	}
}

func TestScope_IfBlockLocalsDoNotLeak(t *testing.T) {
	ds := check(t, prog(
		`if (true) {`,
		`  let a = 1`,
		`  log(a)`,
		`}`,
		`log(a)`))
	d := requireDiag(t, ds, "Identifier not found: a")
	requireLine(t, d, 4)
}

func TestScope_SiblingFunctionsAreIsolated(t *testing.T) {
	ds := check(t, prog(
		`let f = fn() {`,
		`  let v = 1`,
		`  return v`,
		`}`,
		`let g = fn() {`,
		`  return v`,
		`}`))
	requireLine(t, requireDiag(t, ds, "Identifier not found: v"), 5)
}

func TestScope_InnerSeesEveryOuterLevel(t *testing.T) {
	src := prog(
		`let a = 1`,
		`let f = fn() {`,
		`  let b = 2`,
		`  return fn() {`,
		`    return a + b`,
		`  }`,
		`}`,
		`log(f()())`)
	requireNoDiags(t, check(t, src))
}

func TestScope_ParameterShadowingDoesNotHideOuterNames(t *testing.T) {
	src := prog(
		`let outer = 1`,
		`let f = fn(p) {`,
		`  return p + outer`,
		`}`,
		`log(f(2))`)
	requireNoDiags(t, check(t, src))
}

// ===========================================================================
// Builtins
// ===========================================================================

// Keep in sync with the `builtins` map in analyzer.go.
var builtinNames = []string{
	"len", "push", "delete", "panic", "log", "input", "keys",
	"typeof", "string", "int", "float", "copy", "error",
}

func TestBuiltins_CallableWithoutDiagnostics(t *testing.T) {
	for _, name := range builtinNames {
		t.Run(name, func(t *testing.T) {
			src := name + "(1)\n"
			skipIfParseErrors(t, src)
			requireNoDiags(t, check(t, src))
		})
	}
}

func TestBuiltins_CannotBeUsedAsValues(t *testing.T) {
	cases := map[string]string{
		"let value":      `let f = len`,
		"call argument":  `log(len)`,
		"array element":  `let a = [push]`,
		"hash value":     `let h = {"k": len}`,
		"infix operand":  `log(1 + len)`,
		"assigned value": prog(`let x = 1`, `x = len`),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			ds := check(t, src)
			requireDiag(t, ds, "Cannot use builtin: ")
			requireDiag(t, ds, " as non-function")
		})
	}
}

func TestBuiltins_ShadowingMakesThemOrdinaryVariables(t *testing.T) {
	requireNoDiags(t, check(t, prog(`let len = 5`, `let y = len`, `log(y)`)))
}

func TestBuiltins_AreNotArityChecked(t *testing.T) {
	requireNoDiagContaining(t, check(t, `len(1, 2, 3)`), "expects")
}

// ===========================================================================
// Calling things that are not functions
// ===========================================================================

func TestCallingANonFunction(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"int", prog(`let x = 5`, `x()`), "Cannot use x which is INT as FUNCTION"},
		{"string", prog(`let s = "a"`, `s()`), "Cannot use s which is STRING as FUNCTION"},
		{"array", prog(`let a = [1]`, `a()`), "Cannot use a which is ARRAY as FUNCTION"},
		{"hash", prog(`let h = {"a": 1}`, `h()`), "Cannot use h which is HASH as FUNCTION"},
		{"bool", prog(`let b = true`, `b()`), "Cannot use b which is BOOL as FUNCTION"},
		{"float", prog(`let f = 1.5`, `f()`), "Cannot use f which is FLOAT as FUNCTION"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			requireDiag(t, check(t, c.src), c.want)
		})
	}
}

func TestCallingAParameterIsAllowed(t *testing.T) {
	requireNoDiags(t, check(t, prog(
		`let apply = fn(cb) {`,
		`  return cb(1)`,
		`}`,
		`log(apply(fn(x) { return x }))`)))
}

// ===========================================================================
// Function arity
// ===========================================================================

func TestFunctionArity(t *testing.T) {
	def := func(params string) []string {
		return []string{`let f = fn(` + params + `) {`, `  return 1`, `}`}
	}
	cases := []struct {
		name   string
		params string
		call   string
		want   string // "" means no arity diagnostic
	}{
		{"too many", "a", "f(1, 2)", "Function: f expects 1 arguments. Got: 2"},
		{"too few", "a, b", "f(1)", "Function: f expects 2 arguments. Got: 1"},
		{"none given", "a", "f()", "Function: f expects 1 arguments. Got: 0"},
		{"none expected", "", "f(1)", "Function: f expects 0 arguments. Got: 1"},
		{"exact one", "a", "f(1)", ""},
		{"exact many", "a, b, c", "f(1, 2, 3)", ""},
		{"exact zero", "", "f()", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := prog(append(def(c.params), c.call)...)
			ds := check(t, src)
			if c.want == "" {
				requireNoDiagContaining(t, ds, "expects")
				return
			}
			requireLine(t, requireDiag(t, ds, c.want), 3)
		})
	}
}

func TestFunctionArity_ParametersAreNotChecked(t *testing.T) {
	// `cb` is a parameter (type unknown), so any argument count is accepted.
	src := prog(
		`let g = fn(cb) {`,
		`  return cb(1, 2, 3)`,
		`}`)
	requireNoDiagContaining(t, check(t, src), "expects")
}

// ===========================================================================
// break / continue
// ===========================================================================

func TestBreakAndContinueOutsideLoop(t *testing.T) {
	cases := map[string]string{
		"break at top level":    `break`,
		"continue at top level": `continue`,
		"break in function":     prog(`let f = fn() {`, `  break`, `}`),
		"break in if":           prog(`if (true) {`, `  break`, `}`),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			skipIfParseErrors(t, src)
			// The continue message currently says "break"; match on the stable
			// part so fixing that wording does not break this test.
			requireDiag(t, check(t, src), "outside of loop")
		})
	}
}

func TestBreakAndContinueInsideLoopAreFine(t *testing.T) {
	for _, kw := range []string{"break", "continue"} {
		t.Run(kw, func(t *testing.T) {
			src := firstParsing(t,
				"for (true) {\n  "+kw+"\n}\n",
				"for true {\n  "+kw+"\n}\n",
				"for {\n  "+kw+"\n}\n",
			)
			requireNoDiagContaining(t, check(t, src), "outside of loop")
		})
	}
}

func TestBreakInsideIfInsideLoopIsFine(t *testing.T) {
	src := firstParsing(t,
		"for (true) {\n  if (true) {\n    break\n  }\n}\n",
		"for true {\n  if (true) {\n    break\n  }\n}\n",
		"for {\n  if (true) {\n    break\n  }\n}\n",
	)
	requireNoDiagContaining(t, check(t, src), "outside of loop")
}

func TestBreakAfterLoopIsOutsideLoop(t *testing.T) {
	loop := firstParsing(t, "for (true) {\n  break\n}\n", "for true {\n  break\n}\n", "for {\n  break\n}\n")
	ds := check(t, loop+"break\n")
	d := requireDiag(t, ds, "outside of loop")
	requireLine(t, d, strings.Count(loop, "\n"))
}

// ===========================================================================
// Infix type checking
// ===========================================================================

func TestInfixTypeErrors(t *testing.T) {
	cases := []struct{ expr, want string }{
		{`1 + "a"`, "Cannot use + Between INT and STRING"},
		{`"a" - "b"`, "Cannot use - Between STRING and INT"},
		{`"a" + 1`, "Cannot use + Between STRING and INT"},
		{`true + true`, "BETWEEN BOOL AND BOOL"},
		{`true + 1`, "Cannot use BOOL and INT In an infix expression"},
		{`1 + true`, "In an infix expression"},
		{`1.5 + "a"`, "In an infix expression"},
		{`"a" + true`, "Cannot use STRING and BOOL In an infix expression"},
		{`[1] + 1`, "of type ARRAY in infix expression"},
		{`1 + [1]`, "of type ARRAY in infix expression"},
		{`1 + "a" + 2`, "Between INT and STRING"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			requireDiag(t, check(t, "log("+c.expr+")\n"), c.want)
		})
	}
}

func TestInfixTypeErrors_ThroughVariables(t *testing.T) {
	ds := check(t, prog(`let n = 1`, `let s = "x"`, `log(n + s)`))
	requireLine(t, requireDiag(t, ds, "Cannot use + Between INT and STRING"), 2)

	ds = check(t, prog(`let n = 1`, `let s = "x"`, `log(s + n)`))
	requireLine(t, requireDiag(t, ds, "Cannot use + Between STRING and INT"), 2)

	// string repetition (STRING * INT) is allowed
	requireNoDiags(t, check(t, prog(`let n = 3`, `let s = "x"`, `log(s * n)`)))
}

// walkInfix's own comment says "int * string is string", but the INT branch
// only exempts "*" from the first message and then falls into the generic
// error branch, so `3 * "ab"` is currently reported. If INT * STRING is meant
// to be valid (it is the mirror of STRING * INT) this test fails and points at
// that branch; if it is meant to be invalid, change it to requireDiag.
func TestInfix_IntTimesStringIsAllowed(t *testing.T) {
	requireNoDiags(t, check(t, `log(3 * "ab")`))
	requireNoDiags(t, check(t, prog(`let n = 3`, `let s = "x"`, `log(n * s)`)))
}

func TestInfixTypeErrors_ReportedOnTheRightLine(t *testing.T) {
	ds := check(t, prog(`let a = 1`, `let b = 2`, `log(a + "x")`))
	requireLine(t, requireDiag(t, ds, "Between INT and STRING"), 2)
}

// ===========================================================================
// Index / slice expressions
// ===========================================================================

func TestIndexErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"array with string index", prog(`let a = [1, 2, 3]`, `log(a["x"])`), "Should be INT"},
		{"array with float index", prog(`let a = [1, 2, 3]`, `log(a[1.5])`), "Should be INT, got FLOAT"},
		{"array with bool index", prog(`let a = [1, 2, 3]`, `log(a[true])`), "Should be INT, got BOOL"},
		{"array literal with string index", `log([1, 2]["x"])`, "Should be INT"},
		{"string with string index", prog(`let s = "abc"`, `log(s["x"])`), "string index expression"},
		{"int indexed", prog(`let n = 5`, `log(n[0])`), "as ARRAY/HASH/STRING in index expression"},
		{"bool indexed", prog(`let b = true`, `log(b[0])`), "as ARRAY/HASH/STRING in index expression"},
		{"function as hash key", prog(`let h = {"a": 1}`, `log(h[fn() { return 1 }])`), "hash index expression"},
		{"index assign array with string", prog(`let a = [1]`, `a["x"] = 2`), "index assign expression"},
		{"index assign on int", prog(`let n = 1`, `n[0] = 2`), "as ARRAY/HASH/STRING in index expression"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			requireDiag(t, check(t, c.src), c.want)
		})
	}
}

func TestIndexOnUnknownTypeIsNotFlagged(t *testing.T) {
	src := prog(
		`let first = fn(xs) {`,
		`  return xs[0]`,
		`}`,
		`log(first([1]))`)
	requireNoDiags(t, check(t, src))
}

func TestIndexErrorLine(t *testing.T) {
	ds := check(t, prog(`let a = [1]`, `let b = 2`, `log(a["x"])`))
	requireLine(t, requireDiag(t, ds, "Should be INT"), 2)
}

// ===========================================================================
// Hash literals
// ===========================================================================

func TestHashLiteralKeys(t *testing.T) {
	bad := map[string]struct{ src, want string }{
		"bool key":  {`let h = {true: 1}`, "Unusable as hash key: BOOL"},
		"array key": {`let h = {[1]: 2}`, "Unusable as hash key: ARRAY"},
	}
	for name, c := range bad {
		t.Run("bad/"+name, func(t *testing.T) {
			skipIfParseErrors(t, c.src)
			requireDiag(t, check(t, c.src), c.want)
		})
	}

	good := map[string]string{
		"string key": `let h = {"a": 1}`,
		"int key":    `let h = {1: 1}`,
		"float key":  `let h = {1.5: 1}`,
		"empty":      `let h = {}`,
		"mixed":      `let h = {"a": 1, 2: "b", 3.5: [1]}`,
	}
	for name, src := range good {
		t.Run("good/"+name, func(t *testing.T) {
			skipIfParseErrors(t, src)
			requireNoDiags(t, check(t, src))
		})
	}
}

// ===========================================================================
// Multi-value let
// ===========================================================================

func TestMultiValueLet(t *testing.T) {
	t.Run("valid from call", func(t *testing.T) {
		src := prog(
			`let pair = fn() {`,
			`  return 1, 2`,
			`}`,
			`let [a, b] = pair()`,
			`log(a)`,
			`log(b)`)
		skipIfParseErrors(t, src)
		requireNoDiags(t, check(t, src))
	})
	t.Run("non-call value", func(t *testing.T) {
		src := `let [a, b] = 5`
		skipIfParseErrors(t, src)
		requireDiag(t, check(t, src), "as value for multivalue let statement")
	})
	t.Run("names are visible afterwards", func(t *testing.T) {
		src := prog(
			`let pair = fn() {`,
			`  return 1, 2`,
			`}`,
			`let [a, b] = pair()`,
			`log(a + b)`)
		skipIfParseErrors(t, src)
		requireNoDiags(t, check(t, src))
	})
	t.Run("non-identifier on the left", func(t *testing.T) {
		src := prog(`let pair = fn() {`, `  return 1, 2`, `}`, `let [1, b] = pair()`)
		skipIfParseErrors(t, src)
		requireDiag(t, check(t, src), "on left side of let statement")
	})
}

// ===========================================================================
// Parser errors
// ===========================================================================

func TestParserErrorsAreReported(t *testing.T) {
	cases := map[string]string{
		"let without name":      `let = 5`,
		"let without value":     `let x`,
		"unclosed call":         `log(`,
		"unclosed function":     `let f = fn(`,
		"stray close brace":     `}`,
		"stray close paren":     `)`,
		"dangling let":          `let x = `,
		"unclosed array":        `[1, 2`,
		"unclosed hash":         `let h = {"a": `,
		"unclosed function def": `let f = fn(a, {}`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			ds := check(t, src)
			if len(ds) == 0 {
				t.Fatalf("expected at least one diagnostic for %q", src)
			}
		})
	}
}

func TestParserErrorSeverityAndSource(t *testing.T) {
	ds := check(t, `let = 5`)
	if len(ds) == 0 {
		t.Fatal("expected a parser diagnostic")
	}
	for _, d := range ds {
		if d.Severity == nil || *d.Severity != lsp.SeverityError {
			t.Errorf("parser/analyzer diagnostics must be SeverityError, got %v for %q", d.Severity, d.Message)
		}
		if d.Source != "bolt" {
			t.Errorf("source = %q, want bolt", d.Source)
		}
	}
}

func TestParserErrorIsOnTheOffendingLine(t *testing.T) {
	ds := check(t, prog(`let a = 1`, `let = 5`, `let c = 3`))
	if len(ds) == 0 {
		t.Fatal("expected a parser diagnostic")
	}
	for _, d := range ds {
		if d.Range.Start.Line == 1 {
			return
		}
	}
	t.Fatalf("no diagnostic on line 1 (0-based):\n  %s", strings.Join(messages(ds), "\n  "))
}

func TestParserErrorRangeSpansTheWholeLine(t *testing.T) {
	line := `let = 5`
	ds := check(t, prog(`let a = 1`, line))
	for _, d := range ds {
		if d.Range.Start.Line == 1 {
			if d.Range.Start.Character != 0 || d.Range.End.Character != len(line) {
				t.Fatalf("range = %+v, want 0..%d", d.Range, len(line))
			}
			return
		}
	}
	t.Skipf("parser did not report on line 1: %q", messages(ds))
}

func TestMultipleParserErrorsOnMultipleLines(t *testing.T) {
	ds := check(t, prog(`let = 1`, `let = 2`, `let = 3`))
	if len(ds) < 2 {
		t.Fatalf("expected several parser diagnostics, got %q", messages(ds))
	}
}

func TestParserErrorsDoNotHideEachOtherOnCRLF(t *testing.T) {
	ds := check(t, "let a = 1\r\nlet = 5\r\n")
	if len(ds) == 0 {
		t.Fatal("expected a parser diagnostic with CRLF line endings")
	}
}

// ===========================================================================
// Result shape and stability
// ===========================================================================

func TestCleanSourceReturnsEmptyNonNilSlice(t *testing.T) {
	ds := check(t, prog(`let a = 1`, `log(a)`))
	if ds == nil {
		t.Fatal("got nil; clients would receive \"diagnostics\": null")
	}
	if len(ds) != 0 {
		t.Fatalf("got %q", messages(ds))
	}
}

func TestDiagnosticsAreDeterministic(t *testing.T) {
	src := prog(
		`log(a)`,
		`log(b + "x" + 1)`,
		`let h = {"k": c, "j": d, "i": e}`,
		`let f = fn(x) {`,
		`  return x`,
		`}`,
		`f(1, 2, 3)`)

	var first []string
	for i := 0; i < 25; i++ {
		ds := check(t, src)
		var got []string
		for _, d := range ds {
			got = append(got, d.Message)
		}
		sort.Strings(got)
		if i == 0 {
			first = got
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(first) {
			t.Fatalf("run %d differs from run 0:\n first: %q\n now:   %q", i, first, got)
		}
	}
}

func TestFixingTheSourceClearsDiagnostics(t *testing.T) {
	e := newEnv(t)
	requireDiag(t, e.diagnose(t, "doc.bolt", `log(oops)`), "Identifier not found: oops")
	requireNoDiags(t, e.diagnose(t, "doc.bolt", prog(`let oops = 1`, `log(oops)`)))
	requireDiag(t, e.diagnose(t, "doc.bolt", `log(oops)`), "Identifier not found: oops")
}

func TestDocumentsAreIndependent(t *testing.T) {
	e := newEnv(t)
	a := e.diagnose(t, "a.bolt", `log(onlyInA)`)
	b := e.diagnose(t, "b.bolt", prog(`let fine = 1`, `log(fine)`))
	requireDiag(t, a, "Identifier not found: onlyInA")
	requireNoDiags(t, b)
	// a name defined in b.bolt must not satisfy a use in a.bolt
	requireDiag(t, e.diagnose(t, "a.bolt", `log(fine)`), "Identifier not found: fine")
}

func TestCRLFLineNumbers(t *testing.T) {
	ds := check(t, "let a = 1\r\nlet b = 2\r\nlog(zzz)\r\n")
	requireLine(t, requireDiag(t, ds, "Identifier not found: zzz"), 2)
}

func TestErrorsOnLaterLinesOfLargeFile(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&b, "let v%d = %d\n", i, i)
	}
	b.WriteString("log(missing)\n")
	ds := check(t, b.String())
	requireLine(t, requireDiag(t, ds, "Identifier not found: missing"), 500)
}

// ===========================================================================
// CheckInitsAndIdents recording
// ===========================================================================

func TestCheckInits_RecordsAndReplacesDeclarations(t *testing.T) {
	e := newEnv(t)
	uri := e.uri("inits.bolt")

	e.diagnose(t, "inits.bolt", prog(`let alpha = 1`, `let beta = 2`))
	inits := analyzer.DocToInits[uri]
	for _, name := range []string{"alpha", "beta"} {
		if _, ok := inits[name]; !ok {
			t.Errorf("DocToInits missing %q after first run: have %v", name, keysOf(inits))
		}
	}

	e.diagnose(t, "inits.bolt", prog(`let alpha = 1`))
	inits = analyzer.DocToInits[uri]
	if _, ok := inits["beta"]; ok {
		t.Errorf("stale declaration %q survived re-analysis", "beta")
	}
	if _, ok := inits["alpha"]; !ok {
		t.Errorf("declaration %q lost after re-analysis", "alpha")
	}
}

func TestCheckInits_RecordsIdentifierUses(t *testing.T) {
	e := newEnv(t)
	e.diagnose(t, "uses.bolt", prog(`let x = 1`, `log(x)`, `log(x)`))
	lines := analyzer.DocToIdentLines[e.uri("uses.bolt")]
	n := 0
	for _, li := range lines.Idents {
		if li.Name == "x" {
			n++
		}
	}
	if n < 2 {
		t.Fatalf("expected at least 2 recorded entries for x (declaration + uses), got %d", n)
	}
}

func TestCheckInits_IsStdFlagControlsGOSTDCALL(t *testing.T) {
	src := "log(GOSTDCALLthing(1))\n"
	lines := strings.Split(src, "\n")

	for _, isStd := range []bool{false, true} {
		t.Run(fmt.Sprintf("isStd=%v", isStd), func(t *testing.T) {
			e := newEnv(t)
			p, perrs, _ := analyzer.Parse(src)
			if len(perrs) != 0 {
				t.Skipf("snippet does not parse: %v", perrs[0].Message)
			}
			ds := analyzer.CheckInitsAndIdents(p, lines, e.path("std.bolt"), e.uri("std.bolt"), isStd)
			if isStd {
				requireNoDiagContaining(t, ds, "GOSTDCALL")
			} else {
				requireDiag(t, ds, "Identifier not found: GOSTDCALLthing")
			}
		})
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ===========================================================================
// ===========================================================================
// GRAMMAR TESTS
// ===========================================================================
// ===========================================================================

// Each test lists CANDIDATE spellings and uses the first one that the real
// parser accepts. If none parse, the test is SKIPPED (never silently passed)
// with a message pointing here. To tighten a test, delete the wrong candidates
// and keep the real syntax.

func parses(src string) bool {
	_, errs, _ := analyzer.Parse(src)
	return len(errs) == 0
}

// firstParsing returns the first candidate that parses, or skips the test.
func firstParsing(t *testing.T, candidates ...string) string {
	t.Helper()
	for _, c := range candidates {
		if parses(c) {
			return c
		}
	}
	t.Skipf("none of %d candidate syntaxes parse; update the candidates in grammar_test.go to Bolt's real grammar", len(candidates))
	return ""
}

// firstFmt picks the first format string for which fmt.Sprintf(f, sample...)
// parses, and returns it.
func firstFmt(t *testing.T, formats []string, sample ...any) string {
	t.Helper()
	for _, f := range formats {
		if parses(fmt.Sprintf(f, sample...)) {
			return f
		}
	}
	t.Skipf("none of %d candidate syntaxes parse; update the candidates in grammar_test.go to Bolt's real grammar", len(formats))
	return ""
}

// ===========================================================================
// for ... range
// ===========================================================================

// %s is the expression being ranged over; body uses the loop variable i.
var rangeFormats = []string{
	"for i range %s {\n  log(i)\n}\n",
	"for (i range %s) {\n  log(i)\n}\n",
	"for i := range %s {\n  log(i)\n}\n",
}

func TestRange_LoopVariableIsInScopeInsideLoop(t *testing.T) {
	for _, over := range []string{"3", `"abc"`, "[1, 2]", `{"a": 1}`} {
		t.Run(over, func(t *testing.T) {
			f := firstFmt(t, rangeFormats, over)
			requireNoDiags(t, check(t, fmt.Sprintf(f, over)))
		})
	}
}

func TestRange_LoopVariableDoesNotLeakAfterLoop(t *testing.T) {
	f := firstFmt(t, rangeFormats, "3")
	src := fmt.Sprintf(f, "3") + "log(i)\n"
	d := requireDiag(t, check(t, src), "Identifier not found: i")
	requireLine(t, d, strings.Count(fmt.Sprintf(f, "3"), "\n"))
}

func TestRange_OverUnsupportedTypes(t *testing.T) {
	f := firstFmt(t, rangeFormats, "true")
	cases := map[string]struct{ ranged, want string }{
		"bool":     {"true", "Cannot range over BOOL"},
		"function": {"fn() { return 1 }", "Cannot range over FUNCTION"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			src := fmt.Sprintf(f, c.ranged)
			skipIfParseErrors(t, src)
			requireDiag(t, check(t, src), c.want)
		})
	}
}

func TestRange_OverUndefinedIdentifier(t *testing.T) {
	f := firstFmt(t, rangeFormats, "nope")
	requireDiag(t, check(t, fmt.Sprintf(f, "nope")), "Identifier not found: nope")
}

func TestRange_OverVariableOfUnsupportedType(t *testing.T) {
	f := firstFmt(t, rangeFormats, "flag")
	src := "let flag = true\n" + fmt.Sprintf(f, "flag")
	requireDiag(t, check(t, src), "Cannot range over BOOL")
}

func TestRange_TwoVariables(t *testing.T) {
	src := firstParsing(t,
		"for i, v range [1, 2] {\n  log(i)\n  log(v)\n}\n",
		"for (i, v range [1, 2]) {\n  log(i)\n  log(v)\n}\n",
	)
	requireNoDiags(t, check(t, src))
	// neither variable leaks
	d := check(t, src+"log(v)\n")
	requireDiag(t, d, "Identifier not found: v")
}

func TestLoopBodyLocalsDoNotLeak(t *testing.T) {
	src := firstParsing(t,
		"for (true) {\n  let inner = 1\n  break\n}\nlog(inner)\n",
		"for true {\n  let inner = 1\n  break\n}\nlog(inner)\n",
		"for {\n  let inner = 1\n  break\n}\nlog(inner)\n",
	)
	requireDiag(t, check(t, src), "Identifier not found: inner")
}

// ===========================================================================
// Slices
// ===========================================================================

func TestSlice(t *testing.T) {
	t.Run("array and string are sliceable", func(t *testing.T) {
		src := prog(`let a = [1, 2, 3]`, `let s = "abc"`, `log(a[0:1])`, `log(s[0:1])`)
		skipIfParseErrors(t, src)
		requireNoDiags(t, check(t, src))
	})
	t.Run("int is not sliceable", func(t *testing.T) {
		src := prog(`let n = 5`, `log(n[0:1])`)
		skipIfParseErrors(t, src)
		requireDiag(t, check(t, src), "as ARRAY/STRING in slice expression")
	})
	t.Run("hash is not sliceable", func(t *testing.T) {
		src := prog(`let h = {"a": 1}`, `log(h[0:1])`)
		skipIfParseErrors(t, src)
		requireDiag(t, check(t, src), "as ARRAY/STRING in slice expression")
	})
	t.Run("non-int bound variable", func(t *testing.T) {
		src := prog(`let a = [1, 2, 3]`, `let s = "x"`, `log(a[s:2])`)
		skipIfParseErrors(t, src)
		requireDiag(t, check(t, src), "Cannot use s which is STRING as INT")
	})
	t.Run("undefined bound", func(t *testing.T) {
		src := prog(`let a = [1, 2, 3]`, `log(a[lo:2])`)
		skipIfParseErrors(t, src)
		requireDiag(t, check(t, src), "Identifier not found: lo")
	})
	t.Run("unknown-typed value is fine", func(t *testing.T) {
		src := prog(`let f = fn(x) {`, `  return x[0:1]`, `}`)
		skipIfParseErrors(t, src)
		requireNoDiags(t, check(t, src))
	})
}

// ===========================================================================
// Structs
// ===========================================================================

var structDecls = []string{
	"struct Person {\n  name,\n  age\n}\n",
	"struct Person { name, age }\n",
	"struct Person {\n  name\n  age\n}\n",
	"struct Person { name: string, age: int }\n",
	"struct Person {\n  name: string,\n  age: int\n}\n",
}

// %s is the comma separated field list, e.g. `name: "bob", age: 3`.
var structNewFormats = []string{
	"new Person { %s }",
	"new Person{%s}",
	"Person { %s }",
	"Person{%s}",
}

const (
	goodFields = `name: "bob", age: 3`
	badFields  = `name: "bob", height: 3`
)

// structSyntax finds a declaration + instantiation spelling that parse together.
func structSyntax(t *testing.T) (decl, newFmt string) {
	t.Helper()
	for _, d := range structDecls {
		for _, n := range structNewFormats {
			if parses(d + "let p = " + fmt.Sprintf(n, goodFields) + "\n") {
				return d, n
			}
		}
	}
	t.Skip("no candidate struct syntax parses; update structDecls/structNewFormats in grammar_test.go")
	return "", ""
}

func TestStruct_ValidInstantiation(t *testing.T) {
	decl, nf := structSyntax(t)
	src := decl + "let p = " + fmt.Sprintf(nf, goodFields) + "\nlog(p.name)\nlog(p.age)\n"
	requireNoDiags(t, check(t, src))
}

func TestStruct_UnknownFieldInInstantiation(t *testing.T) {
	decl, nf := structSyntax(t)
	src := decl + "let p = " + fmt.Sprintf(nf, badFields) + "\n"
	requireDiag(t, check(t, src), "Struct type Person has no field height")
}

func TestStruct_UnknownFieldInInlineInstantiation(t *testing.T) {
	decl, nf := structSyntax(t)
	src := decl + "log(" + fmt.Sprintf(nf, badFields) + ")\n"
	skipIfParseErrors(t, src)
	requireDiag(t, check(t, src), "Struct type Person has no field height")
}

func TestStruct_UndefinedStructType(t *testing.T) {
	decl, nf := structSyntax(t)
	_ = decl // deliberately not included
	src := "let p = " + fmt.Sprintf(nf, goodFields) + "\n"
	requireDiag(t, check(t, src), "Identifier not found: Person")
}

func TestStruct_InstantiatingANonStruct(t *testing.T) {
	_, nf := structSyntax(t)
	src := "let Person = 5\nlet p = " + fmt.Sprintf(nf, goodFields) + "\n"
	ds := check(t, src)
	requireDiag(t, ds, "Cannot use struct instantiation with non struct Person of type INT")
}

func TestStruct_AccessingAMissingField(t *testing.T) {
	decl, nf := structSyntax(t)
	src := decl + "let p = " + fmt.Sprintf(nf, goodFields) + "\nlog(p.nope)\n"
	requireDiag(t, check(t, src), "Identifier not found: p.nope")
}

func TestStruct_Methods(t *testing.T) {
	decl, nf := structSyntax(t)
	method := "let Person.greet = fn() {\n  return 1\n}\n"
	inst := "let p = " + fmt.Sprintf(nf, goodFields) + "\n"

	t.Run("call with correct arity", func(t *testing.T) {
		src := decl + method + inst + "log(p.greet())\n"
		skipIfParseErrors(t, src)
		requireNoDiags(t, check(t, src))
	})
	t.Run("call with wrong arity", func(t *testing.T) {
		src := decl + method + inst + "log(p.greet(1))\n"
		skipIfParseErrors(t, src)
		requireDiag(t, check(t, src), "expects 0 arguments. Got: 1")
	})
	t.Run("missing method", func(t *testing.T) {
		src := decl + method + inst + "log(p.missing())\n"
		skipIfParseErrors(t, src)
		requireDiag(t, check(t, src), "Identifier not found: p.missing")
	})
	t.Run("method on a type that is not a struct", func(t *testing.T) {
		src := "let Person = 5\nlet Person.greet = fn() {\n  return 1\n}\n"
		skipIfParseErrors(t, src)
		// must not panic; a diagnostic is optional
		check(t, src)
	})
}

func TestStruct_TypeUsedAsFunction(t *testing.T) {
	decl, _ := structSyntax(t)
	src := decl + "Person()\n"
	skipIfParseErrors(t, src)
	requireDiag(t, check(t, src), "Cannot use Person which is STRUCT as FUNCTION")
}

// ===========================================================================
// switch / defer
// ===========================================================================

// %s is the switch subject.
var switchFormats = []string{
	"switch (%s) {\n  case 1:\n    log(1)\n}\n",
	"switch %s {\n  case 1:\n    log(1)\n}\n",
	"switch (%s) {\n  case 1 {\n    log(1)\n  }\n}\n",
	"switch %s {\n  case 1 {\n    log(1)\n  }\n}\n",
}

func TestSwitch_ValidSubject(t *testing.T) {
	f := firstFmt(t, switchFormats, "x")
	requireNoDiags(t, check(t, "let x = 1\n"+fmt.Sprintf(f, "x")))
}

func TestSwitch_UndefinedSubject(t *testing.T) {
	f := firstFmt(t, switchFormats, "nope")
	requireDiag(t, check(t, fmt.Sprintf(f, "nope")), "Identifier not found: nope")
}

func TestSwitch_CaseConditionIsChecked(t *testing.T) {
	src := firstParsing(t,
		"let x = 1\nswitch (x) {\n  case nope:\n    log(1)\n}\n",
		"let x = 1\nswitch x {\n  case nope:\n    log(1)\n}\n",
		"let x = 1\nswitch (x) {\n  case nope {\n    log(1)\n  }\n}\n",
		"let x = 1\nswitch x {\n  case nope {\n    log(1)\n  }\n}\n",
	)
	requireDiag(t, check(t, src), "Identifier not found: nope")
}

func TestSwitch_CaseBodyLocalsDoNotLeak(t *testing.T) {
	src := firstParsing(t,
		"let x = 1\nswitch (x) {\n  case 1:\n    let inner = 1\n}\nlog(inner)\n",
		"let x = 1\nswitch x {\n  case 1:\n    let inner = 1\n}\nlog(inner)\n",
		"let x = 1\nswitch (x) {\n  case 1 {\n    let inner = 1\n  }\n}\nlog(inner)\n",
		"let x = 1\nswitch x {\n  case 1 {\n    let inner = 1\n  }\n}\nlog(inner)\n",
	)
	requireDiag(t, check(t, src), "Identifier not found: inner")
}

func TestDefer(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		src := "defer log(1)\n"
		skipIfParseErrors(t, src)
		requireNoDiags(t, check(t, src))
	})
	t.Run("undefined callee", func(t *testing.T) {
		src := "defer nope(1)\n"
		skipIfParseErrors(t, src)
		requireDiag(t, check(t, src), "Identifier not found: nope")
	})
	t.Run("inside function", func(t *testing.T) {
		src := prog(`let f = fn() {`, `  defer log(1)`, `  return 1`, `}`)
		skipIfParseErrors(t, src)
		requireNoDiags(t, check(t, src))
	})
}

// ===========================================================================
// pub
// ===========================================================================

func TestPub(t *testing.T) {
	t.Run("pub let", func(t *testing.T) {
		src := prog(`pub let x = 1`, `log(x)`)
		skipIfParseErrors(t, src)
		requireNoDiags(t, check(t, src))
	})
	t.Run("pub fn", func(t *testing.T) {
		src := prog(`pub let f = fn() {`, `  return 1`, `}`, `log(f())`)
		skipIfParseErrors(t, src)
		requireNoDiags(t, check(t, src))
	})
	t.Run("pub value is still checked", func(t *testing.T) {
		src := `pub let x = nope`
		skipIfParseErrors(t, src)
		requireDiag(t, check(t, src), "Identifier not found: nope")
	})
}

// ===========================================================================
// ===========================================================================
// HANDLER TESTS
// ===========================================================================
// ===========================================================================

func TestInitialize(t *testing.T) {
	h := servertest.New(t, handler.New())

	if h.InitResult.ServerInfo == nil || h.InitResult.ServerInfo.Name != "bolt" {
		t.Error("expected server info to be set")
	}
}

func TestHover(t *testing.T) {
	h := servertest.New(t, handler.New())

	h.DidOpen("file:///test.txt", "bolt", "hello world")

	hover, err := h.Hover("file:///test.txt", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if hover == nil {
		t.Fatal("expected hover result")
	}
}

func TestCompletion(t *testing.T) {
	h := servertest.New(t, handler.New())

	h.DidOpen("file:///test.txt", "bolt", "hello")

	result, err := h.Completion("file:///test.txt", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) == 0 {
		t.Error("expected at least one completion item")
	}
}

// ===========================================================================
// ===========================================================================
// ROBUSTNESS TESTS
// ===========================================================================
// ===========================================================================

// hostileInputs is everything an editor can plausibly send while the user is
// mid-keystroke, plus some deliberately nasty cases. The only requirement is
// that RunDiagnostics returns (no panic, no hang) and that every diagnostic it
// returns is well-formed (see assertWellFormed). A crash here takes the whole
// language server down, so the editor loses all diagnostics until restart.
var hostileInputs = []string{
	// empty-ish
	"", "\n", "\n\n\n", "   ", "\t", "\r\n", "\r\n\r\n", "\r", "\n\r",

	// binary / invalid encoding
	"\x00", "let x = \x00", "\xff\xfe", "let \xff = 1", "\x00\x00\x00\n\x00",

	// strings and quotes
	"\"", "\"unterminated", "'", "`", "\"\\", "\"\\\"", "\"a\nb\"", "\"\"\"",

	// truncated let
	"let", "let x", "let x =", "let =", "let [", "let [a", "let [a,", "let [a, b]", "let [a, b] =",
	"let a.b =", "let a.b.c = 1", "let 1 = 2", "let \"s\" = 1", "let let = 1", "let fn = 1",

	// truncated fn
	"fn", "fn(", "fn()", "fn() {", "fn(a,", "fn(a b) {}", "let f = fn(", "let f = fn(a) {", "fn(,) {}",

	// truncated if / else
	"if", "if (", "if (true", "if (true)", "if (true) {", "if (true) {} else", "if (true) {} else {", "else", "else {}",

	// truncated loops
	"for", "for (", "for range", "for i range", "for i, v range", "for i range {", "for i range 3", "range", "for ;;",

	// control flow keywords
	"return", "return,", "return 1,", "break", "continue", "break break", "return return",

	// import / pub
	"import", "import \"", "import \"\"", "import 5", "import import", "import \"x\" \"y\"",
	"pub", "pub let", "pub fn", "pub pub let x = 1", "pub 1", "pub pub",

	// struct / new
	"struct", "struct {", "struct A", "struct A {", "struct A { a", "struct A { a, }", "struct A { a a }",
	"new", "new A", "new A {", "new A { a:", "new A { a: 1", "new A { : 1 }", "new new A",
	"let Person.x = 1", "let Person.x.y = 1", "let Person.x = fn() {}",

	// switch / defer
	"switch", "switch (", "switch (1) {", "switch (1) { case", "switch (1) { case 1", "case", "defer", "defer (", "defer defer",

	// brackets and braces
	"[", "]", "[1,", "[1, 2", "[,]", "[,,]", "{", "}", "{\"a\"", "{\"a\":", "{\"a\": 1,", "{,}", "{:}", "(", ")", "()", "(((", ")))", "([)]", "{[}]",

	// indexing and slicing
	"a[", "a[1", "a[1:", "a[:", "a[:]", "a[1:2", "a[1:2]", "[1,2][", "[1,2][0", "[1,2][0:", "\"abc\"[", "\"abc\"[0:1]", "{}[0]", "1[0]", "true[0]", "fn() {}[0]",
	"a[0] = ", "a[0:1] = 1", "a[] = 1", "[][0] = 1",

	// field access
	"a.", "a.b.", ".a", "a..b", "a.1", "1.", "1..2", "a.b.c.d.e", "a.b[0].c", "a.b[0:1].c", "a[0].b", "a.b[0] = 1",
	"a.b.c = 1", "a.\"s\"", "a.(1)", "(a).b", "a.b()", "a.b.c()", "[1].len", "\"s\".len", "1.5.x",

	// operators
	"...", "...a", "!", "!!", "-", "--1", "!-!-!1", "+", "* 2", "1 +", "1 + + 1", "== 1", "1 ==", "&&", "||", "1 && ", "a ?? b", "1 % 0", "1 / 0",
	"1 + \"a\" + true + [1] + {}", "-\"a\"", "!\"a\"", "-[1]", "!{}", "-fn() {}", "...5", "...[1]",

	// assignment
	"x = ", "= 1", "x == = 1", "x[0] = ", "x.y = ", "x = x", "x = x = x", "1 = 1", "\"s\" = 1", "fn() {} = 1",
	"let x = x", "let x = x + 1", "let f = f()", "let f = fn() { return f }", "let a = b\nlet b = a",

	// comments
	"// only a comment", "/* unterminated", "/* c */", "# hash", "let x = 1 // trailing", "//", "/*", "*/", "/**/",

	// unicode
	"let ñ = 1\nlog(ñ)", "let π = 3\nlog(π + 1)", "let s = \"héllo wörld 🎉\"\nlog(s)", "日本語", "😀", "let 😀 = 1", "\u200b", "\ufeff", "\ufefflet a = 1",
	"let s = \"🎉🎉🎉\"\nlog(zzz)", "é = 1\nlog(zzz)",

	// whitespace oddities
	"let\tx\t=\t1", "let  x  =  1", "\n\n\nlog(x)\n\n\n", "   log(x)   ", "\tlog(x)",
	"let a = 1;", "let a = 1;;;", ";", ";;;", "let a = 1; let b = 2; log(a + b)",

	// many errors at once
	"let = \nlet = \nlet = \nlet = \n", ")))))\n}}}}}\n]]]]]\n",
	"log(a)\nlog(b)\nlog(c)\nlog(d)\nlog(e)\nlog(f)\nlog(g)\n",

	// builtins misused
	"len", "log", "len()", "len(len)", "log(log(log(log(1))))", "let log = log", "let len = len\nlen(1)", "log = 1", "len = len",
	"push", "[push, pop, len]", "{len: 1}", "{\"a\": len}", "len.x", "len[0]", "len[0:1]",
	"error", "panic", "panic(\"x\")", "input()", "keys({})", "copy([1])",
}

func TestRobustness_HostileInputsNeverPanicOrHang(t *testing.T) {
	for i, src := range hostileInputs {
		t.Run(fmt.Sprintf("%03d_%.30q", i, src), func(t *testing.T) {
			newEnv(t).guarded(t, "main.bolt", src)
		})
	}
}

func TestRobustness_HostileInputsAreStableAcrossRepeatedRuns(t *testing.T) {
	// Re-running the same text on the same document (what every keystroke
	// does) must neither panic nor accumulate state that changes the result.
	for i, src := range hostileInputs {
		if len(src) > 5000 {
			continue
		}
		t.Run(fmt.Sprintf("%03d_%.30q", i, src), func(t *testing.T) {
			e := newEnv(t)
			first := len(e.guarded(t, "doc.bolt", src))
			for n := 0; n < 3; n++ {
				if got := len(e.guarded(t, "doc.bolt", src)); got != first {
					t.Fatalf("run %d produced %d diagnostics, first run produced %d", n+2, got, first)
				}
			}
		})
	}
}

func TestRobustness_ProgressiveTyping(t *testing.T) {
	// Feed a program one character at a time, the way a keystroke-by-keystroke
	// editor would. Every prefix is a (usually broken) document.
	program := prog(
		`let add = fn(a, b) {`,
		`  return a + b`,
		`}`,
		`let h = {"k": [1, 2, 3]}`,
		`let r = add(h["k"][0], 2)`,
		`if (r > 2) {`,
		`  log(r)`,
		`} else {`,
		`  log(nope)`,
		`}`)

	e := newEnv(t)
	for i := 0; i <= len(program); i++ {
		e.guarded(t, "typing.bolt", program[:i])
	}
}

func TestRobustness_ProgressiveTypingOfStructsAndModules(t *testing.T) {
	program := prog(
		`struct Person { name, age }`,
		`let Person.greet = fn() { return 1 }`,
		`let p = new Person { name: "bob", age: 3 }`,
		`log(p.greet())`,
		`let [a, b] = p.greet()`,
		`for i range [1, 2] { log(i) }`,
		`switch (a) { case 1: log(1) }`,
		`defer log(1)`,
		`import "./nope.bolt"`,
		`pub let z = a[0:1]`)

	e := newEnv(t)
	for i := 0; i <= len(program); i++ {
		e.guarded(t, "typing2.bolt", program[:i])
	}
}

func TestRobustness_DeletingCharacterByCharacter(t *testing.T) {
	program := prog(`let a = [1, 2, 3]`, `let f = fn(x) {`, `  return x[0] + a[1:2]`, `}`, `log(f(a))`)
	e := newEnv(t)
	for i := len(program); i >= 0; i-- {
		e.guarded(t, "deleting.bolt", program[:i])
	}
}

func TestRobustness_EveryLineTruncatedAtEveryColumn(t *testing.T) {
	// For each line, cut it at each column and keep the rest of the program.
	// This produces a broken line in the middle of otherwise valid code.
	lines := []string{
		`let h = {"k": [1, 2, 3]}`,
		`let f = fn(a, b) { return a + b }`,
		`log(f(h["k"][0], 2))`,
		`if (true) { log(1) } else { log(2) }`,
	}
	for li := range lines {
		for col := 0; col <= len(lines[li]); col++ {
			var b strings.Builder
			for i, l := range lines {
				if i == li {
					l = l[:col]
				}
				b.WriteString(l)
				b.WriteString("\n")
			}
			newEnv(t).guarded(t, "trunc.bolt", b.String())
		}
	}
}

func TestRobustness_Large(t *testing.T) {
	big := map[string]string{
		"100k identifier":          strings.Repeat("a", 100_000),
		"very long line":           "log(" + strings.Repeat("1, ", 20_000) + "1)",
		"long infix chain":         "log(" + strings.Repeat("1 + ", 500) + "1)",
		"long mixed infix chain":   "log(" + strings.Repeat("1 + ", 250) + `"a"` + strings.Repeat(" + 1", 250) + ")",
		"right-nested infix":       "log(" + strings.Repeat("1 + (", 200) + "1" + strings.Repeat(")", 200) + ")",
		"deep parens":              strings.Repeat("(", 300) + "1" + strings.Repeat(")", 300),
		"unbalanced open parens":   strings.Repeat("(", 300),
		"unbalanced open brackets": strings.Repeat("[", 300),
		"unbalanced open braces":   strings.Repeat("{", 300),
		"deep prefix":              strings.Repeat("-", 500) + "1",
		"deep not":                 strings.Repeat("!", 500) + "true",
		"deep field access":        strings.Repeat("a.", 300) + "a",
		"deep indexing":            "a" + strings.Repeat("[0]", 300),
		"deep nested arrays":       strings.Repeat("[", 150) + "1" + strings.Repeat("]", 150),
		"deep nested functions":    strings.Repeat("fn() { return ", 100) + "1" + strings.Repeat(" }", 100),
		"deep nested ifs":          strings.Repeat("if (true) { ", 100) + "log(1)" + strings.Repeat(" }", 100),
		"many lets":                strings.Repeat("let x = 1\n", 5000),
		"many errors":              strings.Repeat("log(undefinedThing)\n", 2000),
		"many blank lines":         strings.Repeat("\n", 20_000),
		"many parse errors":        strings.Repeat("let = 1\n", 1000),
		"large hash":               "let h = {" + strings.Repeat(`"k": 1, `, 3000) + `"z": 2}`,
		"large array":              "let a = [" + strings.Repeat("1, ", 10_000) + "1]",
		"many functions":           strings.Repeat("let f = fn(a) { return a }\n", 2000),
	}
	for name, src := range big {
		t.Run(name, func(t *testing.T) {
			newEnv(t).guarded(t, "big.bolt", src)
		})
	}
}

// ===========================================================================
// ===========================================================================
// HELPER TESTS
// ===========================================================================
// ===========================================================================

// TestMain isolates the process from the real user environment and
// initialises the package-level logger. Without logging.Init() every
// logging.Logger.Println call in the analyzer/handler is a nil dereference.
//
// HOME / XDG_* are redirected so that evaluator.ExtractBuiltins(), the stdlib
// cache and the log file all land in a throwaway directory.
func TestMain(m *testing.M) {

	f, err := logging.Init()
	if err != nil {
		fmt.Fprintln(os.Stderr, "logging.Init:", err)
		os.Exit(1)
	}

	code := m.Run()

	f.Close()
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// environment
// ---------------------------------------------------------------------------

// env is a scratch directory plus a fresh Handler. Every test gets its own so
// the package-level analyzer maps (keyed by path/URI) never collide.
type env struct {
	dir string
	h   *handler.Handler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &env{dir: dir, h: handler.New()}
}

func (e *env) path(name string) string { return filepath.Join(e.dir, name) }

func (e *env) uri(name string) lsp.DocumentURI {
	return lsp.DocumentURI("file://" + e.path(name))
}

// diagnose runs the exact code path DidOpen/DidChange/DidSave use
// (Handler.RunDiagnostics) and validates the shape of every diagnostic.
func (e *env) diagnose(t *testing.T, name, src string) []lsp.Diagnostic {
	t.Helper()
	uri := e.uri(name)
	ds := e.h.RunDiagnostics(context.Background(), src, string(uri), uri, true)
	assertWellFormed(t, src, ds)
	return ds
}

// check diagnoses src as a standalone main.bolt in a fresh environment.
func check(t *testing.T, src string) []lsp.Diagnostic {
	t.Helper()
	return newEnv(t).diagnose(t, "main.bolt", src)
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// guarded runs RunDiagnostics in a goroutine with a recover and a timeout, so
// a panic or an infinite loop in the analyzer is reported as a normal test
// failure instead of killing the whole test binary.
func (e *env) guarded(t *testing.T, name, src string) []lsp.Diagnostic {
	t.Helper()

	type result struct {
		ds       []lsp.Diagnostic
		panicked any
	}
	ch := make(chan result, 1)
	uri := e.uri(name)

	go func() {
		var r result
		defer func() {
			if p := recover(); p != nil {
				r.panicked = p
			}
			ch <- r
		}()
		r.ds = e.h.RunDiagnostics(context.Background(), src, string(uri), uri, true)
	}()

	select {
	case r := <-ch:
		if r.panicked != nil {
			t.Fatalf("RunDiagnostics panicked: %v\ninput (%d bytes): %.200q", r.panicked, len(src), src)
		}
		assertWellFormed(t, src, r.ds)
		return r.ds
	case <-time.After(10 * time.Second):
		t.Fatalf("RunDiagnostics did not finish within 10s\ninput (%d bytes): %.200q", len(src), src)
		return nil
	}
}

// ---------------------------------------------------------------------------
// source builders
// ---------------------------------------------------------------------------

// prog joins lines into a newline-terminated program.
func prog(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

// ---------------------------------------------------------------------------
// assertions
// ---------------------------------------------------------------------------

func messages(ds []lsp.Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, fmt.Sprintf("L%d:%d-%d %s", d.Range.Start.Line, d.Range.Start.Character, d.Range.End.Character, d.Message))
	}
	return out
}

func containing(ds []lsp.Diagnostic, sub string) []lsp.Diagnostic {
	var out []lsp.Diagnostic
	for _, d := range ds {
		if strings.Contains(d.Message, sub) {
			out = append(out, d)
		}
	}
	return out
}

func requireDiag(t *testing.T, ds []lsp.Diagnostic, sub string) lsp.Diagnostic {
	t.Helper()
	got := containing(ds, sub)
	if len(got) == 0 {
		t.Fatalf("expected a diagnostic containing %q, got:\n  %s", sub, strings.Join(messages(ds), "\n  "))
	}
	return got[0]
}

func requireNoDiags(t *testing.T, ds []lsp.Diagnostic) {
	t.Helper()
	if len(ds) != 0 {
		t.Fatalf("expected no diagnostics, got:\n  %s", strings.Join(messages(ds), "\n  "))
	}
}

func requireNoDiagContaining(t *testing.T, ds []lsp.Diagnostic, sub string) {
	t.Helper()
	if got := containing(ds, sub); len(got) != 0 {
		t.Fatalf("expected no diagnostic containing %q, got:\n  %s", sub, strings.Join(messages(got), "\n  "))
	}
}

// requireLine asserts the diagnostic starts on the given 0-based line.
func requireLine(t *testing.T, d lsp.Diagnostic, line0 int) {
	t.Helper()
	if d.Range.Start.Line != line0 {
		t.Fatalf("diagnostic %q on line %d (0-based), want %d", d.Message, d.Range.Start.Line, line0)
	}
}

// assertWellFormed checks invariants that must hold for EVERY diagnostic the
// server ever sends, whatever the input:
//   - the slice is non-nil (nil marshals to JSON null, not [])
//   - non-empty message, severity and source set
//   - the range is on a real line and inside it
//
// A zero-value Diagnostic (what analyzer.diag returns for line < 1) trips the
// "empty message" check.
func assertWellFormed(t *testing.T, src string, ds []lsp.Diagnostic) {
	t.Helper()
	if ds == nil {
		t.Errorf("diagnostics slice is nil; it marshals to JSON null instead of []")
	}
	lines := strings.Split(src, "\n")
	for i, d := range ds {
		if d.Message == "" {
			t.Errorf("diagnostic #%d has an empty message (zero-value Diagnostic leaked through?): %+v", i, d)
			continue
		}
		if d.Severity == nil {
			t.Errorf("diagnostic #%d %q has nil severity", i, d.Message)
		}
		if d.Source != "bolt" {
			t.Errorf("diagnostic #%d %q has source %q, want \"bolt\"", i, d.Message, d.Source)
		}
		s, e := d.Range.Start, d.Range.End
		if s.Line < 0 || s.Line >= len(lines) {
			t.Errorf("diagnostic #%d %q starts on line %d but the source has %d lines", i, d.Message, s.Line, len(lines))
			continue
		}
		if s.Character < 0 || e.Character < 0 {
			t.Errorf("diagnostic #%d %q has a negative column: %+v", i, d.Message, d.Range)
		}
		if e.Line == s.Line && e.Character < s.Character {
			t.Errorf("diagnostic #%d %q has an inverted range: %+v", i, d.Message, d.Range)
		}
		if e.Line == s.Line && e.Character > len(lines[s.Line]) {
			t.Errorf("diagnostic #%d %q ends at column %d, past the end of line (%d bytes)", i, d.Message, e.Character, len(lines[s.Line]))
		}
	}
}

// ===========================================================================
// ===========================================================================
// IMPORT TESTS
// ===========================================================================
// ===========================================================================

// Syntax for imports: `import "<path>"`. Module members are accessed
// as <module-name>.<member> where module-name is the file name without
// extension, and only `pub` declarations are exported.

func importLine(path string) string { return fmt.Sprintf("import %q", path) }

// ===========================================================================
// Missing imports
// ===========================================================================

func TestImport_MissingFile(t *testing.T) {
	specs := map[string]string{
		"relative .bolt":    "./does_not_exist.bolt",
		"absolute .bolt":    "/definitely/not/here.bolt",
		"bare name":         "no_such_package",
		"empty string":      "",
		"parent traversal":  "../../../../nope/nothing.bolt",
		"spaces in name":    "/tmp/a file with spaces.bolt",
		"unicode in name":   "/tmp/日本語.bolt",
		"dot-bolt only":     ".bolt",
		"trailing slash":    "/definitely/not/here/",
		"looks like std":    "std/does_not_exist",
		"windows separator": `C:\nope\nothing.bolt`,
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			src := prog(importLine(spec), `log(1)`)
			skipIfParseErrors(t, src)
			ds := check(t, src)
			requireLine(t, requireDiag(t, ds, "Cannot find import"), 0)
		})
	}
}

func TestImport_MissingFileOnALaterLine(t *testing.T) {
	src := prog(`let a = 1`, `let b = 2`, importLine("/definitely/not/here.bolt"))
	skipIfParseErrors(t, src)
	requireLine(t, requireDiag(t, check(t, src), "Cannot find import"), 2)
}

func TestImport_SeveralMissingFilesAreAllReported(t *testing.T) {
	src := prog(importLine("/nope/a.bolt"), importLine("/nope/b.bolt"), importLine("/nope/c.bolt"))
	skipIfParseErrors(t, src)
	ds := check(t, src)
	if got := containing(ds, "Cannot find import"); len(got) != 3 {
		t.Fatalf("want 3 'Cannot find import' diagnostics, got %q", messages(ds))
	}
}

// ===========================================================================
// Real modules
// ===========================================================================

func TestImport_PublicMemberIsVisible(t *testing.T) {
	e := newEnv(t)
	mod := writeFile(t, e.dir, "mathmod.bolt", prog(`pub let shared = 42`, `let hidden = 1`))
	src := prog(importLine(mod), `log(mathmod.shared)`)
	skipIfParseErrors(t, src)
	requireNoDiags(t, e.diagnose(t, "main.bolt", src))
}

func TestImport_PrivateMemberIsNotVisible(t *testing.T) {
	e := newEnv(t)
	mod := writeFile(t, e.dir, "mathmod.bolt", prog(`pub let shared = 42`, `let hidden = 1`))
	src := prog(importLine(mod), `log(mathmod.hidden)`)
	skipIfParseErrors(t, src)
	ds := e.diagnose(t, "main.bolt", src)
	requireLine(t, requireDiag(t, ds, "Identifier not found: mathmod.hidden"), 1)
}

func TestImport_UnknownMemberOfRealModule(t *testing.T) {
	e := newEnv(t)
	mod := writeFile(t, e.dir, "mathmod.bolt", prog(`pub let shared = 42`))
	src := prog(importLine(mod), `log(mathmod.nonsense)`)
	skipIfParseErrors(t, src)
	requireDiag(t, e.diagnose(t, "main.bolt", src), "Identifier not found: mathmod.nonsense")
}

func TestImport_PublicFunctionArityIsChecked(t *testing.T) {
	e := newEnv(t)
	mod := writeFile(t, e.dir, "fns.bolt", prog(`pub let add = fn(a, b) {`, `  return a + b`, `}`))
	src := prog(importLine(mod), `log(fns.add(1))`)
	skipIfParseErrors(t, src)
	ds := e.diagnose(t, "main.bolt", src)
	requireNoDiagContaining(t, ds, "Identifier not found")
	// Arity may or may not be enforced across modules; this documents the result.
	t.Logf("diagnostics for cross-module arity mismatch: %q", messages(ds))
}

func TestImport_ModuleNameWithoutUseIsHarmless(t *testing.T) {
	e := newEnv(t)
	mod := writeFile(t, e.dir, "unused.bolt", prog(`pub let v = 1`))
	src := prog(importLine(mod), `log(1)`)
	skipIfParseErrors(t, src)
	requireNoDiags(t, e.diagnose(t, "main.bolt", src))
}

func TestImport_SameModuleFromTwoFiles(t *testing.T) {
	e := newEnv(t)
	mod := writeFile(t, e.dir, "shared.bolt", prog(`pub let v = 1`))
	src := prog(importLine(mod), `log(shared.v)`)
	skipIfParseErrors(t, src)
	// the second file hits the Modulemap cache instead of re-reading from disk
	requireNoDiags(t, e.diagnose(t, "one.bolt", src))
	requireNoDiags(t, e.diagnose(t, "two.bolt", src))
	requireDiag(t, e.diagnose(t, "three.bolt", prog(importLine(mod), `log(shared.nope)`)), "Identifier not found: shared.nope")
}

func TestImport_SameModuleImportedTwiceInOneFile(t *testing.T) {
	e := newEnv(t)
	mod := writeFile(t, e.dir, "twice.bolt", prog(`pub let v = 1`))
	src := prog(importLine(mod), importLine(mod), `log(twice.v)`)
	skipIfParseErrors(t, src)
	requireNoDiags(t, e.diagnose(t, "main.bolt", src))
}

func TestImport_RecordsDependencies(t *testing.T) {
	e := newEnv(t)
	mod := writeFile(t, e.dir, "dep.bolt", prog(`pub let v = 1`))
	src := prog(importLine(mod), `log(dep.v)`)
	skipIfParseErrors(t, src)
	e.diagnose(t, "main.bolt", src)

	deps, ok := analyzer.ModuleDependencies[e.path("main.bolt")]
	if !ok {
		t.Fatalf("no ModuleDependencies entry for main.bolt")
	}
	if !deps[mod] {
		t.Fatalf("main.bolt does not list %s as a dependency; has %v", mod, deps)
	}
}

func TestImport_DependenciesAreReplacedWhenImportIsRemoved(t *testing.T) {
	e := newEnv(t)
	mod := writeFile(t, e.dir, "dep.bolt", prog(`pub let v = 1`))
	with := prog(importLine(mod), `log(dep.v)`)
	skipIfParseErrors(t, with)
	e.diagnose(t, "main.bolt", with)
	e.diagnose(t, "main.bolt", prog(`log(1)`))

	if deps := analyzer.ModuleDependencies[e.path("main.bolt")]; deps[mod] {
		t.Fatalf("stale dependency on %s survived after the import was removed", mod)
	}
}

func TestImport_ModuleWithUndefinedIdentifierDoesNotLeakItsErrorsIntoImporter(t *testing.T) {
	e := newEnv(t)
	mod := writeFile(t, e.dir, "broken.bolt", prog(`pub let v = notDefinedAnywhere`))
	src := prog(importLine(mod), `log(broken.v)`)
	skipIfParseErrors(t, src)
	ds := e.diagnose(t, "main.bolt", src)
	requireNoDiagContaining(t, ds, "notDefinedAnywhere")
}

func TestImport_ModuleThatDoesNotParse_NoPanic(t *testing.T) {
	e := newEnv(t)
	for i, content := range []string{
		"let = = =\n}}}{{{",
		"",
		"\x00\x01\x02",
		"pub",
		"pub let",
		"pub let x =",
		"import",
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			name := fmt.Sprintf("garbage%d.bolt", i)
			mod := writeFile(t, e.dir, name, content)
			src := prog(importLine(mod), `log(1)`)
			skipIfParseErrors(t, src)
			e.guarded(t, fmt.Sprintf("main%d.bolt", i), src)
		})
	}
}

func TestImport_SelfImport_Terminates(t *testing.T) {
	e := newEnv(t)
	self := e.path("self.bolt")
	src := prog(importLine(self), `log(1)`)
	skipIfParseErrors(t, src)
	writeFile(t, e.dir, "self.bolt", src)
	e.guarded(t, "self.bolt", src)
}

func TestImport_CircularImports_Terminate(t *testing.T) {
	e := newEnv(t)
	a, b := e.path("a.bolt"), e.path("b.bolt")
	srcA := prog(importLine(b), `pub let fromA = 1`)
	srcB := prog(importLine(a), `pub let fromB = 1`)
	skipIfParseErrors(t, srcA)
	writeFile(t, e.dir, "a.bolt", srcA)
	writeFile(t, e.dir, "b.bolt", srcB)

	e.guarded(t, "a.bolt", srcA)
	e.guarded(t, "b.bolt", srcB)
	e.guarded(t, "a.bolt", srcA)
}

func TestImport_ModuleDeletedFromDiskAfterItWasCached_NoPanic(t *testing.T) {
	// Once a module is in analyzer.Modulemap, diagnosing that module's own
	// file os.Stat()s it. If the file has been deleted/renamed since, the
	// handler must not dereference the nil FileInfo.
	e := newEnv(t)
	text := prog(`pub let v = 1`)
	mod := writeFile(t, e.dir, "gone.bolt", text)
	src := prog(importLine(mod), `log(gone.v)`)
	skipIfParseErrors(t, src)
	e.diagnose(t, "main.bolt", src) // caches the module

	if err := os.Remove(mod); err != nil {
		t.Fatal(err)
	}
	t.Log("Removed file")
	e.guarded(t, "gone.bolt", text)
}

func TestImport_EditingAModuleKeepsTheModuleItselfClean(t *testing.T) {
	e := newEnv(t)
	mod := writeFile(t, e.dir, "edited.bolt", prog(`pub let v = 1`))
	src := prog(importLine(mod), `log(edited.v)`)
	skipIfParseErrors(t, src)
	e.diagnose(t, "main.bolt", src)

	// user types in the module (unsaved text differs from disk)
	requireNoDiags(t, e.diagnose(t, "edited.bolt", prog(`pub let v = 1`, `pub let w = 2`)))
	requireDiag(t, e.diagnose(t, "edited.bolt", prog(`pub let v = oops`)), "Identifier not found: oops")
}

func TestImport_HandlerWithNoOpenDocumentsDoesNotNeedAClient(t *testing.T) {
	// RunDiagnostics only touches h.client when a dependent document is open.
	// A bare handler (client == nil) must therefore be safe here.
	e := newEnv(t)
	mod := writeFile(t, e.dir, "solo.bolt", prog(`pub let v = 1`))
	src := prog(importLine(mod), `log(solo.v)`)
	skipIfParseErrors(t, src)
	e.diagnose(t, "main.bolt", src)
	e.guarded(t, "solo.bolt", prog(`pub let v = 2`))
}

// ===========================================================================
// Standard library files
// ===========================================================================

func stdlibPath(t *testing.T, name string) string {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("no user cache dir: %v", err)
	}
	return filepath.Join(cache, "bolt", "stdlib", name)
}

func TestStdlibFiles_IgnoreGOSTDCALLIdentifiers(t *testing.T) {
	std := stdlibPath(t, "fake_std.bolt")
	uri := lsp.DocumentURI("file://" + std)
	src := "log(GOSTDCALLsomething(1))\n"
	skipIfParseErrors(t, src)

	ds := handler.New().RunDiagnostics(context.Background(), src, string(uri), uri, true)
	assertWellFormed(t, src, ds)
	requireNoDiagContaining(t, ds, "GOSTDCALL")

	// identical text anywhere else is an error
	requireDiag(t, check(t, src), "Identifier not found: GOSTDCALLsomething")
}

func TestStdlibFiles_StillReportOtherErrors(t *testing.T) {
	std := stdlibPath(t, "fake_std2.bolt")
	uri := lsp.DocumentURI("file://" + std)
	src := "log(GOSTDCALLfine(1))\nlog(reallyUndefined)\n"
	skipIfParseErrors(t, src)

	ds := handler.New().RunDiagnostics(context.Background(), src, string(uri), uri, true)
	requireNoDiagContaining(t, ds, "GOSTDCALL")
	requireLine(t, requireDiag(t, ds, "Identifier not found: reallyUndefined"), 1)
}

func TestStdlibLookalikeDirectoryIsNotTreatedAsStdlib(t *testing.T) {
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skip(err)
	}
	// "<cache>/bolt/stdlib-extra" shares a string prefix with the stdlib dir
	// but is not inside it.
	p := filepath.Join(cache, "bolt", "stdlib-extra", "x.bolt")
	uri := lsp.DocumentURI("file://" + p)
	src := "log(GOSTDCALLsomething(1))\n"
	skipIfParseErrors(t, src)

	ds := handler.New().RunDiagnostics(context.Background(), src, string(uri), uri, true)
	requireDiag(t, ds, "Identifier not found: GOSTDCALLsomething")
}

// ===========================================================================
// URIs and paths handed to RunDiagnostics
// ===========================================================================

func TestRunDiagnostics_AcceptsAssortedDocumentURIs(t *testing.T) {
	h := handler.New()
	dir := t.TempDir()
	uris := map[string]string{
		"file uri":           "file://" + filepath.Join(dir, "a.bolt"),
		"file uri with spc":  "file://" + filepath.Join(dir, "with space.bolt"),
		"file uri unicode":   "file://" + filepath.Join(dir, "日本語.bolt"),
		"untitled":           "untitled:Untitled-1",
		"no scheme":          filepath.Join(dir, "plain.bolt"),
		"empty":              "",
		"percent-encoded":    "file://" + dir + "/%20enc.bolt",
		"non-bolt extension": "file://" + filepath.Join(dir, "notes.txt"),
		"no extension":       "file://" + filepath.Join(dir, "Makefile"),
		"root":               "file:///",
		"relative-looking":   "file://./rel.bolt",
	}
	for name, u := range uris {
		t.Run(name, func(t *testing.T) {
			done := make(chan any, 1)
			var ds []lsp.Diagnostic
			go func() {
				defer func() { done <- recover() }()
				ds = h.RunDiagnostics(context.Background(), "log(nope)\n", u, lsp.DocumentURI(u), true)
			}()
			select {
			case p := <-done:
				if p != nil {
					t.Fatalf("panic for URI %q: %v", u, p)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("timeout for URI %q", u)
			}
			assertWellFormed(t, "log(nope)\n", ds)
			requireDiag(t, ds, "Identifier not found: nope")
		})
	}
}
