package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
)

const FormatJSON = "json"

func parseLogFormat(format string) error {
	switch format {
	case "", FormatJSON:
		return nil
	default:
		return fmt.Errorf("invalid log format %q: only %q is supported", format, FormatJSON)
	}
}

// newFormatHandler는 formatter handler가 생성되는 유일한 지점이다. 이 함수 밖에서
// formatter를 직접 만들면 그 handler는 sanitize 바깥에 놓여 비정제 record를 받는다.
func newFormatHandler(level slog.Level, w io.Writer) slog.Handler {
	return newSanitizeHandler(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		AddSource:   true,
		ReplaceAttr: shortenSource,
	}))
}

// slog 기본값은 빌드 머신의 절대 경로를 모든 record에 싣는다.
// dir/file 축약으로 빌드 디렉터리 구조 노출과 record 크기를 함께 줄인다.
func shortenSource(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) > 0 || attr.Key != slog.SourceKey {
		return attr
	}
	source, ok := attr.Value.Any().(*slog.Source)
	if !ok {
		return attr
	}
	// PC 0 record는 빈 Source를 낳는다. 빈 Attr을 돌려줘야 slog이 통째로 생략한다.
	// 평탄화 판본은 이 가드가 없으면 ":0"을 실어 생략을 되살리지 못한다.
	if source.File == "" && source.Line == 0 {
		return slog.Attr{}
	}

	var buf [128]byte
	out := append(buf[:0], lastPathSegments(source.File)...)
	out = append(out, ':')
	out = strconv.AppendInt(out, int64(source.Line), 10)
	return slog.String(slog.SourceKey, string(out))
}

// filepath.Join은 Clean 때문에 record마다 할당한다. 여기서는 substring slice로 충분하다.
// 빈 경로에 ""를 돌려주는 것이 load-bearing이다. filepath 판본은 "."를 만들어, File만
// 비어 있고 Line이 살아 있는 record에 ".:42" 같은 허위 경로를 남긴다.
func lastPathSegments(path string) string {
	base := strings.LastIndexByte(path, '/')
	if base < 0 {
		return path
	}
	return path[strings.LastIndexByte(path[:base], '/')+1:]
}
