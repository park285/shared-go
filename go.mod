module github.com/park285/shared-go/v2

go 1.27.3

retract v2.4.1 // exported package removal was incorrectly published as a patch; use v2.5.0

require (
	github.com/jackc/pgx/v5 v5.12.0
	github.com/openai/openai-go/v3 v3.86.0
	github.com/quic-go/quic-go v0.64.1
	github.com/stretchr/testify v1.12.1
	github.com/xdg-go/stringprep v1.0.4
	github.com/ymw0407/jamo v1.0.0
	github.com/yuin/goldmark/v2 v2.1.15
	go.yaml.in/yaml/v3 v3.0.6
	golang.org/x/net v0.62.0
	golang.org/x/sync v0.24.0
	golang.org/x/sys v0.49.0
	golang.org/x/text v0.43.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
)

require (
	github.com/coder/websocket v1.8.15 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/tidwall/gjson v1.20.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.2 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.uber.org/mock v0.6.0 // indirect
	golang.org/x/crypto v0.58.0 // indirect
)
