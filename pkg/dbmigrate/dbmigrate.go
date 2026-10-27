package dbmigrate

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

const (
	// ManifestName은 기본 migration manifest 파일명이다.
	ManifestName = "manifest.txt"
)

// Execer는 migration SQL 실행 함수다.
type Execer func(context.Context, string, ...any) error

// ApplyOptions는 Apply의 선택 동작이다. 영값은 manifest 전체를 ledger·세션 설정 없이 적용한다.
type ApplyOptions struct {
	// Only가 nil이 아니면 그 이름의 migration만 manifest 순서대로 적용한다. nil이 아닌 빈 slice는
	// 아무것도 적용하지 않는 대신 오류다. 운영 적용은 쓰지 않고, 다운스트림 upgrade 경로 테스트가
	// 중간 스키마 상태를 재현할 때 쓰는 선택 적용 계약이다.
	// Manifest에 없는 이름은 세션 설정·ledger 생성 전에 거부하지만, Apply를 WithAdvisoryLock 안에서
	// 호출하는 문서화된 패턴에서는 이 검출이 lock 획득 뒤에 일어난다. Lock 전 fail-fast가 필요하면
	// 호출자가 Manifest()로 사전 검증하라.
	Only []string
	// Ledger가 nil이 아니면 적용 완료 ledger를 써서 이미 기록된 migration을 건너뛰고, 실행한
	// migration을 기록한다. 실행과 기록은 별도 Execer 호출이라 원자적이지 않고 ledger는
	// at-least-once이므로 migration SQL은 idempotent해야 한다. Ledger 단독은 동시 실행을 막지 못한다:
	// 여러 마이그레이터가 같은 migration을 동시에 미적용으로 보고 함께 실행할 수 있다. 다중
	// 레플리카에서 single-flight가 필요하면 Apply를 WithAdvisoryLock으로 감싸라.
	Ledger *Ledger
	// LedgerQuerier는 Ledger 조회에 쓰는 단일 row 조회기다. Ledger를 쓸 때 필수이고, Ledger 없이
	// 지정하면 ledger를 의도한 설정이 조용히 무시되지 않도록 오류다.
	LedgerQuerier RowQuerier
	// Session은 migration 적용 전에 거는 세션 timeout이다. 영값이면 아무것도 설정하지 않는다.
	// 0보다 큰 값을 쓰면 exec는 마이그레이션 전용 pinned connection이어야 한다(SessionConfig 참고).
	Session SessionConfig
}

// Manifest는 manifest.txt를 파싱해 적용 순서대로 SQL 파일명을 반환한다.
func Manifest(fsys fs.FS) (names []string, err error) {
	file, err := fsys.Open(ManifestName)
	if err != nil {
		return nil, fmt.Errorf("dbmigrate: open manifest: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("dbmigrate: close manifest: %w", closeErr)
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
			return nil, fmt.Errorf("dbmigrate: parse manifest line: %w", parseErr)
		}

		if prev, ok := seenOrders[order]; ok {
			return nil, fmt.Errorf("dbmigrate: manifest line %d: duplicate order %q (first seen at line %d)", lineNo, order, prev)
		}

		if prev, ok := seenNames[name]; ok {
			return nil, fmt.Errorf("dbmigrate: manifest line %d: duplicate filename %q (first seen at line %d)", lineNo, name, prev)
		}

		if lastOrder != "" && !orderAscends(lastOrder, order) {
			return nil, fmt.Errorf("dbmigrate: manifest line %d: order %q must be greater than previous order %q", lineNo, order, lastOrder)
		}

		seenOrders[order] = lineNo
		seenNames[name] = lineNo
		lastOrder = order

		names = append(names, name)
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("dbmigrate: scan manifest: %w", scanErr)
	}

	if len(names) == 0 {
		return nil, fmt.Errorf("dbmigrate: manifest %q has no entries", ManifestName)
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
func Apply(ctx context.Context, fsys fs.FS, exec Execer, opts ApplyOptions) error {
	if exec == nil {
		return errors.New("dbmigrate: exec is required")
	}

	// 옵션 조합은 세션 설정·ledger 생성 같은 부작용 전에 검증한다.
	if err := opts.validateLedger(); err != nil {
		return err
	}

	entries, err := Manifest(fsys)
	if err != nil {
		return err
	}

	// nil 집합은 전체 적용이다. Only를 지정했으면 부작용 전에 목록부터 검증한다.
	var only map[string]bool

	if opts.Only != nil {
		if only, err = selectOnly(opts.Only, entries); err != nil {
			return fmt.Errorf("dbmigrate: validate only: %w", err)
		}
	}

	if err := opts.Session.Configure(ctx, exec); err != nil {
		return fmt.Errorf("dbmigrate: configure session: %w", err)
	}

	if err := opts.prepareLedger(ctx, exec); err != nil {
		return fmt.Errorf("dbmigrate: prepare ledger: %w", err)
	}

	for _, name := range entries {
		if only != nil && !only[name] {
			continue
		}

		shouldApply, shouldApplyErr := opts.shouldApply(ctx, name)
		if shouldApplyErr != nil {
			return fmt.Errorf("dbmigrate: should apply: %w", shouldApplyErr)
		}

		if !shouldApply {
			continue
		}

		if err := opts.applyEntry(ctx, fsys, exec, name); err != nil {
			return fmt.Errorf("dbmigrate: apply entry: %w", err)
		}
	}

	return nil
}

// selectOnly는 nil이 아닌 Only 목록을 검증해 조회용 집합으로 바꾼다.
func selectOnly(names, entries []string) (map[string]bool, error) {
	if len(names) == 0 {
		return nil, errors.New("dbmigrate: Only requires at least one migration name")
	}

	only := make(map[string]bool, len(names))
	for _, name := range names {
		only[name] = true
	}

	var missing []string

	for name := range only {
		if !slices.Contains(entries, name) {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return only, nil
	}

	slices.Sort(missing)

	return nil, fmt.Errorf("dbmigrate: Only names not in manifest: %s", strings.Join(missing, ", "))
}

func (o ApplyOptions) validateLedger() error {
	switch {
	case o.Ledger == nil && o.LedgerQuerier != nil:
		return errors.New("dbmigrate: LedgerQuerier requires Ledger")
	case o.Ledger != nil && o.LedgerQuerier == nil:
		return errors.New("dbmigrate: ledger querier is required")
	default:
		return nil
	}
}

func (o ApplyOptions) prepareLedger(ctx context.Context, exec Execer) error {
	if o.Ledger == nil {
		return nil
	}

	if err := o.Ledger.Ensure(ctx, exec); err != nil {
		return fmt.Errorf("ensure: %w", err)
	}

	return nil
}

func (o ApplyOptions) shouldApply(ctx context.Context, name string) (bool, error) {
	if o.Ledger == nil {
		return true, nil
	}

	applied, err := o.Ledger.Applied(ctx, o.LedgerQuerier, name)
	if err != nil {
		return false, fmt.Errorf("applied: %w", err)
	}

	return !applied, nil
}

func (o ApplyOptions) applyEntry(ctx context.Context, fsys fs.FS, exec Execer, name string) error {
	sqlBytes, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("dbmigrate: read %s: %w", name, err)
	}

	if err := exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("dbmigrate: exec %s: %w", name, err)
	}

	if o.Ledger == nil {
		return nil
	}

	if err := o.Ledger.Record(ctx, exec, name); err != nil {
		return fmt.Errorf("record: %w", err)
	}

	return nil
}
