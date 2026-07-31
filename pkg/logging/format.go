package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/lmittmann/tint"
)

const (
	FormatText = "text"
	FormatJSON = "json"
)

type logFormat int

const (
	formatText logFormat = iota
	formatJSON
)

func parseLogFormat(format string) (logFormat, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", FormatText:
		return formatText, nil
	case FormatJSON:
		return formatJSON, nil
	default:
		return formatText, fmt.Errorf("invalid log format %q: want %q or %q", format, FormatText, FormatJSON)
	}
}

// newFormatHandler는 formatter handler가 생성되는 유일한 지점이다. 포맷을 추가하거나
// 바꾸더라도 모든 반환 경로가 newSanitizeHandler를 통과해야 한다. 이 함수 밖에서
// formatter를 직접 만들면 그 handler는 sanitize 바깥에 놓여 비정제 record를 받는다.
func newFormatHandler(format logFormat, level slog.Level, w io.Writer, noColor bool) slog.Handler {
	switch format {
	case formatJSON:
		return newSanitizeHandler(slog.NewJSONHandler(w, &slog.HandlerOptions{
			Level:       level,
			AddSource:   true,
			ReplaceAttr: shortenSource,
		}))
	case formatText:
		return newSanitizeHandler(tint.NewTextHandler(w, &tint.Options{
			Level:      level,
			TimeFormat: time.RFC3339,
			AddSource:  true,
			NoColor:    noColor,
		}))
	default:
		panic(fmt.Sprintf("logging: unhandled log format %d (parseLogFormat is the only valid producer)", format))
	}
}

// slog 기본값은 빌드 머신의 절대 경로를 모든 record에 싣는다. tint text lane과 같은
// dir/file 축약으로 맞춰 빌드 디렉터리 구조 노출과 record 크기를 함께 줄인다.
func shortenSource(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) > 0 || attr.Key != slog.SourceKey {
		return attr
	}
	source, ok := attr.Value.Any().(*slog.Source)
	if !ok {
		return attr
	}
	return slog.Any(slog.SourceKey, &slog.Source{
		Function: source.Function,
		File:     lastPathSegments(source.File),
		Line:     source.Line,
	})
}

// filepath.Join은 Clean 때문에 record마다 할당한다. 여기서는 substring slice로 충분하다.
// 빈 경로에 ""를 돌려주는 것이 load-bearing이다. filepath 판본은 "."를 만들어, PC 0 record가
// 낳는 빈 Source를 slog이 생략하지 못하게 되살린다.
func lastPathSegments(path string) string {
	base := strings.LastIndexByte(path, '/')
	if base < 0 {
		return path
	}
	return path[strings.LastIndexByte(path[:base], '/')+1:]
}
