package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/BobdaProgrammer/bolt/lsp/logging"
	"github.com/BobdaProgrammer/bolt/token"
	"github.com/owenrumney/go-lsp/lsp"
)

// logging.Logger is only initialised by logging.Init() in main(); without it
// every Logger.Println in the analyzer is a nil dereference.
func TestMain(m *testing.M) {

	f, err := logging.Init()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	f.Close()
	os.Exit(code)
}

// ===========================================================================
// diag()
// ===========================================================================

func TestDiag_LineBelowOneReturnsZeroValue(t *testing.T) {
	// Callers must never forward this; handler tests assert no empty-message
	// diagnostic ever leaves RunDiagnostics.
	for _, line := range []int{0, -1, -100} {
		d := diag("msg", line, []string{"x"}, 0)
		if d.Message != "" || d.Severity != nil {
			t.Errorf("line %d: expected zero-value Diagnostic, got %+v", line, d)
		}
	}
}

func TestDiag_WithoutTokenCoversWholeLine(t *testing.T) {
	d := diag("boom", 2, []string{"first", "second line"}, 0)
	if d.Range.Start.Line != 1 || d.Range.End.Line != 1 {
		t.Fatalf("line = %d..%d, want 1..1", d.Range.Start.Line, d.Range.End.Line)
	}
	if d.Range.Start.Character != 0 || d.Range.End.Character != len("second line") {
		t.Fatalf("range = %+v, want 0..%d", d.Range, len("second line"))
	}
	if d.Message != "boom" || d.Source != "bolt" {
		t.Fatalf("message/source wrong: %+v", d)
	}
}

func TestDiag_WithTokenCoversTheToken(t *testing.T) {
	// `let abc = 1` -> abc is columns 4..7 (0-based, half-open); the lexer
	// reports Column as the 1-based column of the token's last character.
	d := diag("m", 1, []string{"let abc = 1"}, 0, token.Token{Column: 7, Literal: "abc"})
	if d.Range.Start.Character != 4 || d.Range.End.Character != 7 {
		t.Fatalf("range = %+v, want 4..7", d.Range)
	}
}

func TestDiag_TokenRangeIsClamped(t *testing.T) {
	t.Run("start clamped at 0", func(t *testing.T) {
		d := diag("m", 1, []string{"abcdef"}, 0, token.Token{Column: 2, Literal: "abcdef"})
		if d.Range.Start.Character != 0 {
			t.Fatalf("start = %d, want 0", d.Range.Start.Character)
		}
	})
	t.Run("end clamped to line length", func(t *testing.T) {
		d := diag("m", 1, []string{"abc"}, 0, token.Token{Column: 99, Literal: "c"})
		if d.Range.End.Character != 3 {
			t.Fatalf("end = %d, want 3", d.Range.End.Character)
		}
	})
	t.Run("empty line", func(t *testing.T) {
		d := diag("m", 1, []string{""}, 0, token.Token{Column: 5, Literal: "x"})
		if d.Range.End.Character != 0 || d.Range.Start.Character < 0 {
			t.Fatalf("range = %+v", d.Range)
		}
	})
	t.Run("zero token", func(t *testing.T) {
		d := diag("m", 1, []string{"abc"}, 0, token.Token{})
		if d.Range.Start.Character != 0 || d.Range.End.Character != 0 {
			t.Fatalf("range = %+v", d.Range)
		}
	})
}

func TestDiag_Severity(t *testing.T) {
	if d := diag("m", 1, []string{"x"}, 0); d.Severity == nil || *d.Severity != lsp.SeverityError {
		t.Errorf("sev 0 should be error, got %v", d.Severity)
	}
	if d := diag("m", 1, []string{"x"}, 1); d.Severity == nil || *d.Severity != lsp.SeverityWarning {
		t.Errorf("sev 1 should be warning, got %v", d.Severity)
	}
	if d := diag("m", 1, []string{"x"}, 42); d.Severity == nil || *d.Severity != lsp.SeverityError {
		t.Errorf("unknown severity should default to error, got %v", d.Severity)
	}
}

func TestDiag_LineBeyondEndOfFile_DoesNotPanic(t *testing.T) {
	// Defensive: the lexer should never report a line past the end of the
	// text, but diag() indexes lines[line-1] unguarded, so a single off-by-one
	// upstream would crash the server. Expect either a clamped diagnostic or
	// the zero value, but no panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("diag panicked for line past EOF: %v", r)
		}
	}()
	diag("m", 5, []string{"only one line"}, 0)
	diag("m", 1, []string{}, 0)
}

// ===========================================================================
// getStartColumn
// ===========================================================================

func TestGetStartColumn(t *testing.T) {
	cases := []struct {
		col  int
		lit  string
		want int
	}{
		{1, "x", 1},
		{7, "abc", 5},
		{3, "abc", 1},
		{10, "", 11}, // empty literal: documents the formula Column-(len-1)
	}
	for _, c := range cases {
		if got := getStartColumn(token.Token{Column: c.col, Literal: c.lit}); got != c.want {
			t.Errorf("getStartColumn(col=%d, %q) = %d, want %d", c.col, c.lit, got, c.want)
		}
	}
}

// ===========================================================================
// Scope
// ===========================================================================

func TestScope_LookupAndExists(t *testing.T) {
	outer := NewScope()
	outer.vals["a"] = &Initialised{Kind: INT, Line: 1}
	inner := NewEnclosedScope(outer)
	inner.vals["b"] = &Initialised{Kind: STRING, Line: 2}

	if !inner.Exists("a") || !inner.Exists("b") {
		t.Error("inner scope must see its own and outer names")
	}
	if outer.Exists("b") {
		t.Error("outer scope must not see inner names")
	}
	if inner.Exists("missing") || outer.Exists("missing") {
		t.Error("missing names must not exist")
	}
	if got := inner.Lookup("a"); got.Kind != INT {
		t.Errorf("Lookup(a).Kind = %v, want INT", got.Kind)
	}
}

func TestScope_LookupMissingReturnsUnknownSentinel(t *testing.T) {
	got := NewScope().Lookup("nope")
	if got == nil {
		t.Fatal("Lookup must never return nil")
	}
	if got.Kind != UNKNOWN || got.Line != -1 || got.Column != -1 {
		t.Fatalf("sentinel = %+v, want Kind=UNKNOWN Line=-1 Column=-1", got)
	}
}

func TestScope_InnerShadowsOuter(t *testing.T) {
	outer := NewScope()
	outer.vals["x"] = &Initialised{Kind: INT}
	inner := NewEnclosedScope(outer)
	inner.vals["x"] = &Initialised{Kind: STRING}

	if inner.Lookup("x").Kind != STRING {
		t.Error("inner x must shadow outer x")
	}
	if outer.Lookup("x").Kind != INT {
		t.Error("outer x must be unchanged")
	}
}

func TestScope_DeepChain(t *testing.T) {
	root := NewScope()
	root.vals["deep"] = &Initialised{Kind: FLOAT}
	s := root
	for i := 0; i < 1000; i++ {
		s = NewEnclosedScope(s)
	}
	if !s.Exists("deep") || s.Lookup("deep").Kind != FLOAT {
		t.Fatal("lookup through 1000 enclosing scopes failed")
	}
}

func TestNewScope_StartsEmpty(t *testing.T) {
	s := NewScope()
	if s.outer != nil || len(s.vals) != 0 || len(s.idents) != 0 {
		t.Fatalf("new scope not empty: %+v", s)
	}
	e := NewEnclosedScope(s)
	if e.outer != s || len(e.vals) != 0 || len(e.idents) != 0 {
		t.Fatalf("enclosed scope wrong: %+v", e)
	}
}

// ===========================================================================
// IsNil
// ===========================================================================

func TestIsNil(t *testing.T) {
	var nilScope *Scope
	var nilMap map[string]int
	var nilSlice []int
	var nilFunc func()
	var nilChan chan int
	var nilIface any

	nilCases := map[string]any{
		"untyped nil":  nil,
		"nil iface":    nilIface,
		"typed nil *T": nilScope,
		"nil map":      nilMap,
		"nil slice":    nilSlice,
		"nil func":     nilFunc,
		"nil chan":     nilChan,
	}
	for name, v := range nilCases {
		if !IsNil(v) {
			t.Errorf("IsNil(%s) = false, want true", name)
		}
	}

	nonNil := map[string]any{
		"pointer":      &Scope{},
		"empty map":    map[string]int{},
		"empty slice":  []int{},
		"zero int":     0,
		"empty string": "",
		"false":        false,
		"struct":       Scope{},
	}
	for name, v := range nonNil {
		if IsNil(v) {
			t.Errorf("IsNil(%s) = true, want false", name)
		}
	}
}

// ===========================================================================
// CanonicalPath
// ===========================================================================

func TestCanonicalPath(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct{ in, want string }{
		{"a.bolt", filepath.Join(wd, "a.bolt")},
		{"./a.bolt", filepath.Join(wd, "a.bolt")},
		{"sub/../a.bolt", filepath.Join(wd, "a.bolt")},
		{"sub//deeper/./b.bolt", filepath.Join(wd, "sub", "deeper", "b.bolt")},
		{"", wd},
		{".", wd},
		{string(filepath.Separator) + "abs" + string(filepath.Separator) + "x.bolt", string(filepath.Separator) + filepath.Join("abs", "x.bolt")},
		{string(filepath.Separator) + "abs" + string(filepath.Separator) + ".." + string(filepath.Separator) + "y.bolt", string(filepath.Separator) + "y.bolt"},
	}
	for _, c := range cases {
		got, err := CanonicalPath(c.in)
		if err != nil {
			t.Errorf("CanonicalPath(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("CanonicalPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCanonicalPath_IsIdempotent(t *testing.T) {
	once, _ := CanonicalPath("x/../y/./z.bolt")
	twice, _ := CanonicalPath(once)
	if once != twice {
		t.Fatalf("not idempotent: %q then %q", once, twice)
	}
}

// ===========================================================================
// GetImport for files that do not exist
// ===========================================================================

func TestGetImport_MissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.bolt")

	t.Run("reports a diagnostic on the import line", func(t *testing.T) {
		program, name, d, isStd, dest := GetImport(missing, 7)
		if program != nil {
			t.Error("program must be nil for a missing file")
		}
		if name != "" || isStd || dest != "" {
			t.Errorf("name=%q isStd=%v dest=%q, want zero values", name, isStd, dest)
		}
		if d.Line != 7 || d.Message != "Cannot find import" {
			t.Errorf("diagnostic = %+v, want line 7 'Cannot find import'", d)
		}
	})

	t.Run("line -1 suppresses the diagnostic", func(t *testing.T) {
		_, _, d, _, _ := GetImport(missing, -1)
		if d.Line != -1 || d.Message != "" {
			t.Errorf("diagnostic = %+v, want Line=-1 and no message", d)
		}
	})
}

func TestGetImport_ExistingModule(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mod.bolt")
	if err := os.WriteFile(p, []byte("pub let v = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	program, name, d, isStd, _ := GetImport(p, 1)
	if program == nil {
		t.Fatal("expected a parsed program")
	}
	if name == "" {
		t.Error("expected a module name")
	}
	if isStd {
		t.Error("a user file must not be reported as stdlib")
	}
	if d.Line != -1 || d.Message != "" {
		t.Errorf("unexpected diagnostic for an existing file: %+v", d)
	}
}

// ===========================================================================
// InitTypeToString
// ===========================================================================

func TestInitTypeToString_NamesEveryType(t *testing.T) {
	// Every InitType value shows up in user-facing messages such as
	// "Cannot use x which is <TYPE> as FUNCTION". A missing entry produces an
	// empty string and a garbled message.
	for k := INT; k <= UNKNOWN; k++ {
		if InitTypeToString[k] == "" {
			t.Errorf("InitTypeToString has no name for InitType(%d)", k)
		}
	}
}

func TestInitTypeToString_NamesAreUnique(t *testing.T) {
	seen := map[string]InitType{}
	for k, v := range InitTypeToString {
		if prev, ok := seen[v]; ok {
			t.Errorf("types %d and %d share the name %q", prev, k, v)
		}
		seen[v] = k
	}
}

// ===========================================================================
// Parse
// ===========================================================================

func TestParse_ReturnsNonNilSliceAndProgram(t *testing.T) {
	for _, src := range []string{"", "\n", "let a = 1", "let = ", "((("} {
		program, diags, comments := Parse(src)
		if program == nil {
			t.Errorf("%q: nil program", src)
		}
		if diags == nil {
			t.Errorf("%q: nil diagnostics (marshals to null)", src)
		}
		if comments == nil {
			t.Errorf("%q: nil comment map", src)
		}
	}
}

func TestParse_ErrorLinesAreInsideTheDocument(t *testing.T) {
	srcs := []string{
		"let = 5", "let = 5\n", "\n\nlet = 5", "log(", "log(\n", "{", "{\n\n", "[1,\n2,\n", "if (true) {\n",
		"let a = 1\r\nlet = 5\r\n", "let x = \"unterminated", "let x = \"unterminated\n",
	}
	for _, src := range srcs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Parse(%q) panicked: %v", src, r)
				}
			}()
			_, diags, _ := Parse(src)
			n := len(splitLines(src))
			for _, d := range diags {
				if d.Range.Start.Line < 0 || d.Range.Start.Line >= n {
					t.Errorf("Parse(%q): diagnostic on line %d of %d: %s", src, d.Range.Start.Line, n, d.Message)
				}
			}
		}()
	}
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}
