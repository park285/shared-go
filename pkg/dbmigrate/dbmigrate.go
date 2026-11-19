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
	ManifestName = "manifest.txt"
)

type Execer func(context.Context, string, ...any) error

type ApplyOptions struct {
	Only          []string
	Ledger        *Ledger
	LedgerQuerier RowQuerier
	Session       SessionConfig
}

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

func Apply(ctx context.Context, fsys fs.FS, exec Execer, opts ApplyOptions) error {
	if exec == nil {
		return errors.New("dbmigrate: exec is required")
	}

	if err := opts.validateLedger(); err != nil {
		return err
	}

	entries, err := Manifest(fsys)
	if err != nil {
		return err
	}

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
