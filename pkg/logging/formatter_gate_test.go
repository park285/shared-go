package logging

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

var formatterConstructors = map[string]struct{}{
	"log/slog.NewJSONHandler":                  {},
	"log/slog.NewTextHandler":                  {},
	"github.com/lmittmann/tint.NewHandler":     {},
	"github.com/lmittmann/tint.NewTextHandler": {},
}

var formatterCallAllowlist = map[string]string{
	"format.go|newFormatHandler|log/slog.NewJSONHandler":                  "유일한 formatter 생성 지점 (json)",
	"format.go|newFormatHandler|github.com/lmittmann/tint.NewTextHandler": "유일한 formatter 생성 지점 (text)",
	"logging.go|NewUnsanitizedLoggerForTests|log/slog.NewTextHandler":     "비정제 test logger — 이름으로 오용을 드러낸다",
}

const (
	fileScopeOwner  = "<file-scope>"
	funcLitOwnerTag = ".<func-literal>"
)

type formatterCall struct {
	site string
	pos  string
}

func TestFormatterConstructorsStayInsideFormatHandler(t *testing.T) {
	t.Parallel()

	root := loggingPackageDir(t)
	found := collectFormatterCalls(t, root)

	seen := make(map[string]bool, len(formatterCallAllowlist))
	for _, call := range found {
		if _, ok := formatterCallAllowlist[call.site]; !ok {
			t.Errorf("formatter 생성자가 허용 지점 밖에서 참조된다: %s (%s)\n"+
				"직접 호출이든 함수 값으로 넘기든, 그렇게 만들어진 handler는 newSanitizeHandler 바깥에 놓여 "+
				"비정제 record를 받는다. newFormatHandler를 경유하거나, 의도된 예외면 "+
				"formatterCallAllowlist에 사유와 함께 등록하라.",
				call.site, call.pos)
			continue
		}
		seen[call.site] = true
	}

	for site, reason := range formatterCallAllowlist {
		if !seen[site] {
			t.Errorf("allowlist 항목이 더 이상 존재하지 않는다: %s (%s) — 항목을 제거하라", site, reason)
		}
	}
}

func TestFormatterGate_FileScopeCallDoesNotInheritPrecedingFuncName(t *testing.T) {
	t.Parallel()

	const src = `package logging

import (
	"io"
	"log/slog"
)

func newFormatHandler() slog.Handler { return nil }

var leakyHandler = slog.NewJSONHandler(io.Discard, nil)

func shortenSource() {}
`

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "format.go", src, 0)
	if err != nil {
		t.Fatalf("parse synthetic source: %v", err)
	}

	calls := formatterCallsInFile(fset, parsed, "format.go")
	if len(calls) != 1 {
		t.Fatalf("formatterCallsInFile() found %d calls, want 1: %v", len(calls), calls)
	}
	if _, allowed := formatterCallAllowlist[calls[0].site]; allowed {
		t.Fatalf("file-scope call inherited an allowlisted function name: %s", calls[0].site)
	}
	if want := strings.Join([]string{"format.go", fileScopeOwner, "log/slog.NewJSONHandler"}, "|"); calls[0].site != want {
		t.Fatalf("site = %q, want %q", calls[0].site, want)
	}
}

func TestFormatterGate_FuncLiteralDoesNotInheritEnclosingFuncName(t *testing.T) {
	t.Parallel()

	const src = `package logging

import (
	"io"
	"log/slog"
)

var hoisted func(io.Writer) slog.Handler

func newFormatHandler() slog.Handler {
	hoisted = func(out io.Writer) slog.Handler { return slog.NewJSONHandler(out, nil) }

	return nil
}
`

	calls := parseFormatterCalls(t, "format.go", src)
	if len(calls) != 1 {
		t.Fatalf("formatterCallsInFile() found %d calls, want 1: %v", len(calls), calls)
	}
	if _, allowed := formatterCallAllowlist[calls[0].site]; allowed {
		t.Fatalf("func literal inherited an allowlisted owner: %s", calls[0].site)
	}
	if want := strings.Join([]string{"format.go", "newFormatHandler" + funcLitOwnerTag, "log/slog.NewJSONHandler"}, "|"); calls[0].site != want {
		t.Fatalf("site = %q, want %q", calls[0].site, want)
	}
}

func TestFormatterGate_DetectsDotImportAndFunctionValueForms(t *testing.T) {
	t.Parallel()

	const src = `package logging

import (
	. "log/slog"
	"io"
	tinted "github.com/lmittmann/tint"
)

func dotImported() Handler { return NewJSONHandler(io.Discard, nil) }

func functionValue() {
	build := tinted.NewTextHandler
	_ = build(io.Discard, nil)
}
`

	calls := parseFormatterCalls(t, "probe.go", src)
	got := make(map[string]bool, len(calls))
	for _, call := range calls {
		got[call.site] = true
	}

	for _, want := range []string{
		"probe.go|dotImported|log/slog.NewJSONHandler",
		"probe.go|functionValue|github.com/lmittmann/tint.NewTextHandler",
	} {
		if !got[want] {
			t.Errorf("gate missed %s; found %v", want, calls)
		}
	}
	if len(calls) != 2 {
		t.Errorf("formatterCallsInFile() found %d refs, want 2: %v", len(calls), calls)
	}
}

func TestFormatterGate_MethodOwnerCarriesReceiverType(t *testing.T) {
	t.Parallel()

	const src = `package logging

import (
	"io"
	"log/slog"
)

type alpha struct{}

type beta struct{}

func (a alpha) build() slog.Handler { return slog.NewJSONHandler(io.Discard, nil) }

func (b *beta) build() slog.Handler { return slog.NewJSONHandler(io.Discard, nil) }
`

	calls := parseFormatterCalls(t, "probe.go", src)
	if len(calls) != 2 {
		t.Fatalf("formatterCallsInFile() found %d refs, want 2: %v", len(calls), calls)
	}
	if calls[0].site == calls[1].site {
		t.Fatalf("same-named methods on different receivers collapsed to one site key: %s", calls[0].site)
	}
	for i, want := range []string{"probe.go|alpha.build|", "probe.go|*beta.build|"} {
		if !strings.HasPrefix(calls[i].site, want) {
			t.Errorf("site[%d] = %q, want prefix %q", i, calls[i].site, want)
		}
	}
}

func parseFormatterCalls(t *testing.T, name, src string) []formatterCall {
	t.Helper()

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("parse synthetic source: %v", err)
	}

	return formatterCallsInFile(fset, parsed, name)
}

func loggingPackageDir(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}

	return filepath.Dir(file)
}

func collectFormatterCalls(t *testing.T, root string) []formatterCall {
	t.Helper()

	var calls []formatterCall
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		parsed, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		calls = append(calls, formatterCallsInFile(fset, parsed, rel)...)

		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}

	return calls
}

// 호출이 아니라 참조를 센다. 호출식만 보면 `f := slog.NewJSONHandler; f(...)`가 빠져나간다.
// 선언별로 내려가야 한다. 파일 전체를 한 번에 훑으면서 FuncDecl로만 소유자를 갱신하면
// package-level var 초기화식이 바로 앞 함수의 이름을 물려받아 allowlist에 얹힌다.
func formatterCallsInFile(fset *token.FileSet, file *ast.File, rel string) []formatterCall {
	imports, dotImports := importPathsByName(file)

	var calls []formatterCall

	for _, decl := range file.Decls {
		calls = append(calls, formatterRefsUnder(fset, decl, rel, declOwner(decl), imports, dotImports)...)
	}

	return calls
}

// 함수 리터럴은 소유자를 물려받으면 안 된다. allowlist된 함수 안에서 리터럴로 raw formatter를
// 만들어 package var로 반출하면 site key가 allowlist 항목과 같아져 그대로 통과한다.
func formatterRefsUnder(
	fset *token.FileSet,
	root ast.Node,
	rel, owner string,
	imports map[string]string,
	dotImports []string,
) []formatterCall {
	var (
		calls  []formatterCall
		nested []*ast.FuncLit
	)

	ast.Inspect(root, func(node ast.Node) bool {
		if lit, ok := node.(*ast.FuncLit); ok && lit != root {
			nested = append(nested, lit)

			return false
		}

		qualified, descend := formatterRefName(node, imports, dotImports)
		if qualified != "" {
			calls = append(calls, formatterCall{
				site: strings.Join([]string{rel, owner, qualified}, "|"),
				pos:  fset.Position(node.Pos()).String(),
			})
		}

		return descend
	})

	for _, lit := range nested {
		calls = append(calls, formatterRefsUnder(fset, lit, rel, owner+funcLitOwnerTag, imports, dotImports)...)
	}

	return calls
}

// package 이름으로 한정된 selector는 더 내려갈 것이 없다. 내려가면 Sel ident가 dot-import
// 후보로 한 번 더 잡혀 같은 참조가 두 번 계상된다.
func formatterRefName(node ast.Node, imports map[string]string, dotImports []string) (string, bool) {
	switch expr := node.(type) {
	case *ast.SelectorExpr:
		pkg, ok := expr.X.(*ast.Ident)
		if !ok {
			return "", true
		}
		path, ok := imports[pkg.Name]
		if !ok {
			return "", true
		}

		return matchedConstructor(path + "." + expr.Sel.Name), false
	case *ast.Ident:
		for _, path := range dotImports {
			if qualified := matchedConstructor(path + "." + expr.Name); qualified != "" {
				return qualified, true
			}
		}
	}

	return "", true
}

func matchedConstructor(qualified string) string {
	if _, ok := formatterConstructors[qualified]; !ok {
		return ""
	}

	return qualified
}

func declOwner(decl ast.Decl) string {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok {
		return fileScopeOwner
	}
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}

	return types.ExprString(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func importPathsByName(file *ast.File) (map[string]string, []string) {
	imports := make(map[string]string, len(file.Imports))

	var dotImports []string

	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if spec.Name != nil && spec.Name.Name == "." {
			dotImports = append(dotImports, path)

			continue
		}

		name := path[strings.LastIndex(path, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}

		imports[name] = path
	}

	return imports, dotImports
}
