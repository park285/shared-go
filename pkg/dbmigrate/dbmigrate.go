package dbmigrate

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

const (
	// ManifestName은 기본 migration manifest 파일명이다.
	ManifestName = "manifest.txt"
)

// Execer는 migration SQL 실행 함수다.
type Execer func(context.Context, string, ...any) error

// SQLExecer는 database/sql 계열 ExecContext 최소 인터페이스다.
type SQLExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Option은 migration 적용 동작을 조정한다.
type Option func(*options)

type options struct {
	only          map[string]bool
	ledger        *Ledger
	ledgerQuerier RowQuerier
	session       *SessionConfig
}

// SQLExec는 database/sql 계열 handle을 Execer로 감싼다.
func SQLExec(db SQLExecer) Execer {
	return func(ctx context.Context, query string, args ...any) error {
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("exec context: %w", err)
		}

		return nil
	}
}

// WithOnly는 지정한 migration 파일명만 manifest 순서대로 적용한다.
// 이름을 하나도 넘기지 않으면 Apply는 아무것도 적용하지 않는 대신 오류를 반환한다.
// Manifest에 없는 이름은 Apply가 세션 설정·ledger 생성 전에 거부하지만, Apply를
// WithAdvisoryLock 안에서 호출하는 문서화된 패턴에서는 이 검출이 lock 획득 뒤에
// 일어난다. Lock 전 fail-fast가 필요하면 호출자가 Manifest()로 사전 검증하라.
func WithOnly(names ...string) Option {
	return func(o *options) {
		o.only = make(map[string]bool, len(names))
		for _, name := range names {
			o.only[name] = true
		}
	}
}

// Manifest는 manifest.txt를 파싱해 적용 순서대로 SQL 파일명을 반환한다.
func Manifest(fsys fs.FS) (names []string, err error) {
	file, err := fsys.Open(ManifestName)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close manifest: %w", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	lineNo := 0
	seenOrders := make(map[string]int)
	seenNames := make(map[string]int)
	lastOrder := ""

	for scanner.Scan() {
		lineNo++

		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		order, name, parseErr := parseManifestLine(lineNo, line)
		if parseErr != nil {
			return nil, fmt.Errorf("parse manifest line: %w", parseErr)
		}

		if prev, ok := seenOrders[order]; ok {
			return nil, fmt.Errorf("manifest line %d: duplicate order %q (first seen at line %d)", lineNo, order, prev)
		}

		if prev, ok := seenNames[name]; ok {
			return nil, fmt.Errorf("manifest line %d: duplicate filename %q (first seen at line %d)", lineNo, name, prev)
		}

		if lastOrder != "" && !orderAscends(lastOrder, order) {
			return nil, fmt.Errorf("manifest line %d: order %q must be greater than previous order %q", lineNo, order, lastOrder)
		}

		seenOrders[order] = lineNo
		seenNames[name] = lineNo
		lastOrder = order

		names = append(names, name)
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("scan manifest: %w", scanErr)
	}

	if len(names) == 0 {
		return nil, fmt.Errorf("manifest %q has no entries", ManifestName)
	}

	return names, nil
}

func parseManifestLine(lineNo int, line string) (order, name string, err error) {
	fields := strings.Fields(line)
	if len(fields) != 2 {
		return "", "", fmt.Errorf("manifest line %d: malformed %q (want \"NNN filename.sql\")", lineNo, line)
	}

	order, name = fields[0], fields[1]
	if !isDecimalOrder(order) {
		return "", "", fmt.Errorf("manifest line %d: order %q must contain only decimal digits", lineNo, order)
	}

	if !fs.ValidPath(name) || strings.Contains(name, "/") || !strings.HasSuffix(name, ".sql") {
		return "", "", fmt.Errorf("manifest line %d: migration %q must be a single .sql filename", lineNo, name)
	}

	return order, name, nil
}

// 적용 순서는 manifest의 줄 순서다. 순서 토큰이 그 순서와 어긋나면 토큰이 잘못된 안전감만 주므로,
// 토큰이 줄 순서와 일치함을 적재 시점에 강제한다.
func orderAscends(previous, current string) bool {
	prev := strings.TrimLeft(previous, "0")
	cur := strings.TrimLeft(current, "0")

	if len(prev) != len(cur) {
		return len(prev) < len(cur)
	}

	return prev < cur
}

func isDecimalOrder(value string) bool {
	if value == "" {
		return false
	}

	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// Apply는 manifest 순서대로 SQL 파일을 읽어 exec로 실행한다.
func Apply(ctx context.Context, fsys fs.FS, exec Execer, opts ...Option) error {
	if exec == nil {
		return errors.New("dbmigrate: exec is required")
	}

	cfg := applyOptions(opts)

	entries, err := Manifest(fsys)
	if err != nil {
		return fmt.Errorf("dbmigrate: %w", err)
	}

	if err := cfg.validateOnly(entries); err != nil {
		return fmt.Errorf("validate only: %w", err)
	}

	if err := cfg.configureSession(ctx, exec); err != nil {
		return fmt.Errorf("configure session: %w", err)
	}

	if err := cfg.prepareLedger(ctx, exec); err != nil {
		return fmt.Errorf("prepare ledger: %w", err)
	}

	for _, name := range entries {
		shouldApply, shouldApplyErr := cfg.shouldApply(ctx, name)
		if shouldApplyErr != nil {
			return fmt.Errorf("should apply: %w", shouldApplyErr)
		}

		if !shouldApply {
			continue
		}

		if err := cfg.applyEntry(ctx, fsys, exec, name); err != nil {
			return fmt.Errorf("apply entry: %w", err)
		}
	}

	return nil
}

func applyOptions(opts []Option) options {
	var cfg options

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return cfg
}

func (o options) validateOnly(entries []string) error {
	if o.only == nil {
		return nil
	}

	if len(o.only) == 0 {
		return errors.New("dbmigrate: WithOnly requires at least one migration name")
	}

	present := make(map[string]bool, len(entries))
	for _, name := range entries {
		present[name] = true
	}

	var missing []string

	for name := range o.only {
		if !present[name] {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	sort.Strings(missing)

	return fmt.Errorf("dbmigrate: WithOnly names not in manifest: %s", strings.Join(missing, ", "))
}

func (o options) configureSession(ctx context.Context, exec Execer) error {
	if o.session == nil {
		return nil
	}

	if err := o.session.Configure(ctx, exec); err != nil {
		return fmt.Errorf("configure: %w", err)
	}

	return nil
}

func (o options) prepareLedger(ctx context.Context, exec Execer) error {
	if o.ledger == nil {
		return nil
	}

	if o.ledgerQuerier == nil {
		return errors.New("dbmigrate: ledger querier is required")
	}

	if err := o.ledger.Ensure(ctx, exec); err != nil {
		return fmt.Errorf("ensure: %w", err)
	}

	return nil
}

func (o options) shouldApply(ctx context.Context, name string) (bool, error) {
	if o.only != nil && !o.only[name] {
		return false, nil
	}

	if o.ledger == nil {
		return true, nil
	}

	applied, err := o.ledger.Applied(ctx, o.ledgerQuerier, name)
	if err != nil {
		return false, fmt.Errorf("applied: %w", err)
	}

	return !applied, nil
}

func (o options) applyEntry(ctx context.Context, fsys fs.FS, exec Execer, name string) error {
	sqlBytes, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("dbmigrate: read %s: %w", name, err)
	}

	if err := exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("dbmigrate: exec %s: %w", name, err)
	}

	if o.ledger != nil {
		if err := o.ledger.Record(ctx, exec, name); err != nil {
			return fmt.Errorf("record: %w", err)
		}

		return nil
	}

	return nil
}
