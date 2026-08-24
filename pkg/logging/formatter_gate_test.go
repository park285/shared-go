package logging

import (
	"go/parser"
	"go/token"
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
