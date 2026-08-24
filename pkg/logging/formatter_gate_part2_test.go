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

// 호출이 아니라 참조를 센다. 호출식만 보면 `f := slog.NewJSONHandler; f(...)`가 빠져나간다.
// 선언별로 내려가야 한다. 파일 전체를 한 번에 훑으면서 FuncDecl로만 소유자를 갱신하면
// package-level var 초기화식이 바로 앞 함수의 이름을 물려받아 allowlist에 얹힌다.
func formatterCallsInFile(fset *token.FileSet, file *ast.File, rel string) []formatterCall {
	imports, dotImports := importPathsByName(file)

	calls := make([]formatterCall, 0, len(file.Decls))

	for _, decl := range file.Decls {
		calls = append(calls, formatterRefsUnder(fset, decl, rel, declOwner(decl), imports, dotImports)...)
	}

	return calls
}

// 함수 리터럴은 소유자를 물려받으면 안 된다. Allowlist된 함수 안에서 리터럴로 raw formatter를
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
