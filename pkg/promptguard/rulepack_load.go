package promptguard

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

func loadRulepacks(dir string, logger *slog.Logger) ([]compiledPack, error) {
	paths := findRulepackFiles(dir)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no rulepacks found in %s", dir)
	}

	packs := make([]compiledPack, 0, len(paths))
	for _, path := range paths {
		data, err := readRulepackFile(dir, path)
		if err != nil {
			return nil, fmt.Errorf("read rulepack %s: %w", path, err)
		}

		pack, err := loadRulepack(data)
		if err != nil {
			return nil, fmt.Errorf("load rulepack %s: %w", path, err)
		}

		packs = append(packs, pack)

		if logger != nil {
			logger.Info(
				"rulepack_loaded",
				slog.String("path", path),
				slog.Float64("review_threshold", pack.Policy.ReviewThreshold),
				slog.Float64("block_threshold", pack.Policy.BlockThreshold),
				slog.Int("rule_count", len(pack.Rules)),
			)
		}
	}

	return packs, nil
}

func loadRulepacksFS(fsys fs.FS, root string, logger *slog.Logger) ([]compiledPack, error) {
	cleanRoot, err := cleanRulepackFSRoot(root)
	if err != nil {
		return nil, err
	}

	paths := findRulepackFSFiles(fsys, cleanRoot)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no rulepacks found in %s", cleanRoot)
	}

	packs := make([]compiledPack, 0, len(paths))
	for _, rulepackPath := range paths {
		data, err := fs.ReadFile(fsys, rulepackPath)
		if err != nil {
			return nil, fmt.Errorf("read rulepack %s: %w", rulepackPath, err)
		}

		pack, err := loadRulepack(data)
		if err != nil {
			return nil, fmt.Errorf("load rulepack %s: %w", rulepackPath, err)
		}

		packs = append(packs, pack)

		if logger != nil {
			logger.Info(
				"rulepack_loaded",
				slog.String("path", rulepackPath),
				slog.Float64("review_threshold", pack.Policy.ReviewThreshold),
				slog.Float64("block_threshold", pack.Policy.BlockThreshold),
				slog.Int("rule_count", len(pack.Rules)),
			)
		}
	}

	return packs, nil
}

func readRulepackFile(baseDir, path string) ([]byte, error) {
	cleanBase := filepath.Clean(baseDir)
	cleanPath := filepath.Clean(path)

	rel, err := filepath.Rel(cleanBase, cleanPath)
	if err != nil {
		return nil, fmt.Errorf("resolve relative path: %w", err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("path %q escapes base dir %q", cleanPath, cleanBase)
	}

	info, err := os.Lstat(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("rulepack %q is a symlink", cleanPath)
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return data, nil
}

func loadRulepack(data []byte) (compiledPack, error) {
	var root yaml.Node

	if unmarshalErr := yaml.Unmarshal(data, &root); unmarshalErr != nil {
		return compiledPack{}, fmt.Errorf("parse yaml node: %w", unmarshalErr)
	}

	if lintErr := lintRulepackNode(&root); lintErr != nil {
		return compiledPack{}, fmt.Errorf("lint rulepack: %w", lintErr)
	}

	var raw rawRulepack

	if unmarshalErr := yaml.Unmarshal(data, &raw); unmarshalErr != nil {
		return compiledPack{}, fmt.Errorf("parse rulepack: %w", unmarshalErr)
	}

	pack, compileErr := compileRulepack(&raw)
	if compileErr != nil {
		return compiledPack{}, fmt.Errorf("compile rulepack: %w", compileErr)
	}

	return pack, nil
}

func findRulepackFiles(dir string) []string {
	var files []string

	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d == nil || d.IsDir() {
			return nil
		}

		lower := strings.ToLower(d.Name())
		if strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml") {
			files = append(files, path)
		}

		return nil
	}); err != nil {
		return nil
	}

	slices.Sort(files)

	return files
}

func cleanRulepackFSRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" || root == "." {
		return ".", nil
	}

	clean := path.Clean(root)
	if clean == "." {
		return ".", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("rulepack root %q escapes fs root", root)
	}

	return clean, nil
}

func findRulepackFSFiles(fsys fs.FS, root string) []string {
	var files []string

	if err := fs.WalkDir(fsys, root, func(rulepackPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d == nil || d.IsDir() {
			return nil
		}

		lower := strings.ToLower(d.Name())
		if strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml") {
			files = append(files, rulepackPath)
		}

		return nil
	}); err != nil {
		return nil
	}

	slices.Sort(files)

	return files
}
