package logging

import (
	"fmt"
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
	"format.go|newFormatHandler|log/slog.NewJSONHandler": "유일한 formatter 생성 지점 (json)",
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
			return fmt.Errorf("parse file: %w", parseErr)
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return fmt.Errorf("rel: %w", relErr)
		}

		calls = append(calls, formatterCallsInFile(fset, parsed, rel)...)

		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}

	return calls
}

// 함수 값 참조도 검사하며, 파일 범위 초기화식의 소유자는 함수와 구분한다.
func formatterCallsInFile(fset *token.FileSet, file *ast.File, rel string) []formatterCall {
	imports, dotImports := importPathsByName(file)

	calls := make([]formatterCall, 0, len(file.Decls))

	for _, decl := range file.Decls {
		calls = append(calls, formatterRefsUnder(fset, decl, rel, declOwner(decl), imports, dotImports)...)
	}

	return calls
}

// 함수 리터럴은 별도 소유자로 검사하여 외부로 반출되는 formatter도 제한한다.
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

// Selector 내부 식별자를 dot-import 참조로 중복 집계하지 않는다.
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

		name := path
		if _, suffix, found := strings.CutLast(path, "/"); found {
			name = suffix
		}

		if spec.Name != nil {
			name = spec.Name.Name
		}

		imports[name] = path
	}

	return imports, dotImports
}
