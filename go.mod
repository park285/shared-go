module github.com/park285/shared-go/v2

go 1.27.4

retract v2.4.1 // exported package removal was incorrectly published as a patch; use v2.5.0

require (
	github.com/stretchr/testify v1.12.1
	github.com/ymw0407/jamo v1.0.0
	github.com/yuin/goldmark/v2 v2.1.16
	go.yaml.in/yaml/v3 v3.0.6
	golang.org/x/net v0.62.0
	golang.org/x/sync v0.25.0
	golang.org/x/sys v0.50.0
	golang.org/x/text v0.44.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
)
