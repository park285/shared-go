package promptguard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type loadedRulepack struct {
	path      string
	raw       rawRulepack
	hasKind   bool
	hasPolicy bool
	hasRules  bool
}

func loadConfiguredRulepacks(cfg Config, logger *slog.Logger) (compiledRulepackSet, error) {
	if cfg.RulepackFS != nil && strings.TrimSpace(cfg.RulepacksDir) != "" {
		return compiledRulepackSet{}, fmt.Errorf("configure only one RulepacksDir or RulepackFS")
	}

	var (
		set compiledRulepackSet
		err error
	)
	if cfg.UseEmbeddedDefaults {
		set, err = loadEmbeddedConfiguredRulepacks(cfg)
	} else {
		set, err = loadStandaloneConfiguredRulepacks(cfg)
	}
	if err != nil {
		return compiledRulepackSet{}, err
	}

	if err := assignPolicyDigest(&set); err != nil {
		return compiledRulepackSet{}, err
	}
	logRulepackSet(logger, set)

	return set, nil
}

func loadEmbeddedConfiguredRulepacks(cfg Config) (compiledRulepackSet, error) {
	base, err := loadRulepackSetFS(defaultRulepackFS, defaultRulepacksRoot)
	if err != nil {
		return compiledRulepackSet{}, fmt.Errorf("load embedded rulepacks: %w", err)
	}
	if base.Version != 3 {
		return compiledRulepackSet{}, fmt.Errorf("embedded defaults must use rulepack v3")
	}
	overlay, configured, err := loadConfiguredV3Overlay(cfg, base.Policy)
	if err != nil {
		return compiledRulepackSet{}, err
	}
	if !configured {
		return base, nil
	}

	return combineV3Sets(base, overlay)
}

func loadConfiguredV3Overlay(cfg Config, policy compiledPolicy) (compiledRulepackSet, bool, error) {
	if cfg.RulepackFS != nil {
		overlay, err := loadRulepackOverlayFS(cfg.RulepackFS, configuredRulepackRoot(cfg.RulepackRoot), policy)
		if err != nil {
			return compiledRulepackSet{}, true, fmt.Errorf("load rulepack overlay: %w", err)
		}

		return overlay, true, nil
	}
	if strings.TrimSpace(cfg.RulepacksDir) != "" {
		overlay, err := loadRulepackOverlayDir(cfg.RulepacksDir, policy)
		if err != nil {
			return compiledRulepackSet{}, true, fmt.Errorf("load rulepack overlay: %w", err)
		}

		return overlay, true, nil
	}

	return compiledRulepackSet{}, false, nil
}

func loadStandaloneConfiguredRulepacks(cfg Config) (compiledRulepackSet, error) {
	var (
		set compiledRulepackSet
		err error
	)
	if cfg.RulepackFS != nil {
		set, err = loadRulepackSetFS(cfg.RulepackFS, configuredRulepackRoot(cfg.RulepackRoot))
	} else if strings.TrimSpace(cfg.RulepacksDir) != "" {
		set, err = loadRulepackSetDir(cfg.RulepacksDir)
	} else {
		return compiledRulepackSet{}, fmt.Errorf("enabled guard requires RulepacksDir, RulepackFS, or UseEmbeddedDefaults")
	}
	if err != nil {
		return compiledRulepackSet{}, err
	}
	return set, nil
}

func configuredRulepackRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return "."
	}

	return root
}

func assignPolicyDigest(set *compiledRulepackSet) error {
	digest, err := computePolicyDigest(*set)
	if err != nil {
		return fmt.Errorf("compute effective policy digest: %w", err)
	}
	if digest == "" {
		return fmt.Errorf("compute effective policy digest: empty digest")
	}
	set.Digest = digest

	return nil
}

func loadRulepackSetDir(dir string) (compiledRulepackSet, error) {
	paths := findRulepackFiles(dir)
	if len(paths) == 0 {
		return compiledRulepackSet{}, fmt.Errorf("no rulepacks found in %s", dir)
	}

	files := make([]loadedRulepack, 0, len(paths))
	for _, rulepackPath := range paths {
		data, err := readRulepackFile(dir, rulepackPath)
		if err != nil {
			return compiledRulepackSet{}, fmt.Errorf("read rulepack %s: %w", rulepackPath, err)
		}
		file, err := decodeRulepackFile(rulepackPath, data)
		if err != nil {
			return compiledRulepackSet{}, fmt.Errorf("load rulepack %s: %w", rulepackPath, err)
		}
		files = append(files, file)
	}

	return compileRulepackSet(files)
}

func loadRulepackSetFS(fsys fs.FS, root string) (compiledRulepackSet, error) {
	cleanRoot, err := cleanRulepackFSRoot(root)
	if err != nil {
		return compiledRulepackSet{}, err
	}
	paths := findRulepackFSFiles(fsys, cleanRoot)
	if len(paths) == 0 {
		return compiledRulepackSet{}, fmt.Errorf("no rulepacks found in %s", cleanRoot)
	}

	files := make([]loadedRulepack, 0, len(paths))
	for _, rulepackPath := range paths {
		data, err := fs.ReadFile(fsys, rulepackPath)
		if err != nil {
			return compiledRulepackSet{}, fmt.Errorf("read rulepack %s: %w", rulepackPath, err)
		}
		file, err := decodeRulepackFile(rulepackPath, data)
		if err != nil {
			return compiledRulepackSet{}, fmt.Errorf("load rulepack %s: %w", rulepackPath, err)
		}
		files = append(files, file)
	}

	return compileRulepackSet(files)
}

func loadRulepackOverlayDir(dir string, policy compiledPolicy) (compiledRulepackSet, error) {
	paths := findRulepackFiles(dir)
	if len(paths) == 0 {
		return compiledRulepackSet{}, fmt.Errorf("no rulepacks found in %s", dir)
	}

	files := make([]loadedRulepack, 0, len(paths))
	for _, rulepackPath := range paths {
		data, err := readRulepackFile(dir, rulepackPath)
		if err != nil {
			return compiledRulepackSet{}, fmt.Errorf("read rulepack %s: %w", rulepackPath, err)
		}
		file, err := decodeRulepackFile(rulepackPath, data)
		if err != nil {
			return compiledRulepackSet{}, fmt.Errorf("load rulepack %s: %w", rulepackPath, err)
		}
		files = append(files, file)
	}

	return compileV3Overlay(files, policy)
}

func loadRulepackOverlayFS(fsys fs.FS, root string, policy compiledPolicy) (compiledRulepackSet, error) {
	cleanRoot, err := cleanRulepackFSRoot(root)
	if err != nil {
		return compiledRulepackSet{}, err
	}
	paths := findRulepackFSFiles(fsys, cleanRoot)
	if len(paths) == 0 {
		return compiledRulepackSet{}, fmt.Errorf("no rulepacks found in %s", cleanRoot)
	}

	files := make([]loadedRulepack, 0, len(paths))
	for _, rulepackPath := range paths {
		data, err := fs.ReadFile(fsys, rulepackPath)
		if err != nil {
			return compiledRulepackSet{}, fmt.Errorf("read rulepack %s: %w", rulepackPath, err)
		}
		file, err := decodeRulepackFile(rulepackPath, data)
		if err != nil {
			return compiledRulepackSet{}, fmt.Errorf("load rulepack %s: %w", rulepackPath, err)
		}
		files = append(files, file)
	}

	return compileV3Overlay(files, policy)
}

func compileRulepackSet(files []loadedRulepack) (compiledRulepackSet, error) {
	if len(files) == 0 {
		return compiledRulepackSet{}, fmt.Errorf("rulepack set is empty")
	}

	version := files[0].raw.Version
	if version != 3 {
		return compiledRulepackSet{}, fmt.Errorf("rulepack version must be 3")
	}
	for _, file := range files[1:] {
		if file.raw.Version != version {
			return compiledRulepackSet{}, fmt.Errorf("mixed rulepack versions are unsupported")
		}
	}
	return compileV3Set(files)
}

func compileV3Set(files []loadedRulepack) (compiledRulepackSet, error) {
	policyIndex := -1
	for i, file := range files {
		if err := validateV3File(file); err != nil {
			return compiledRulepackSet{}, err
		}
		if file.raw.Kind == rulepackKindPolicy {
			if policyIndex >= 0 {
				return compiledRulepackSet{}, fmt.Errorf("rulepack v3 requires exactly one policy file")
			}
			policyIndex = i
		}
	}
	if policyIndex < 0 {
		return compiledRulepackSet{}, fmt.Errorf("rulepack v3 requires exactly one policy file")
	}

	policyRaw := files[policyIndex].raw
	if err := validateV3Policy(policyRaw.Policy); err != nil {
		return compiledRulepackSet{}, fmt.Errorf("%s: %w", files[policyIndex].path, err)
	}
	policy := compilePolicy(&policyRaw)

	rules := make([]compiledRule, 0)
	seen := make(map[string]struct{})
	for _, file := range files {
		if file.raw.Kind != rulepackKindRules {
			continue
		}
		pack, err := compileRulepack(&file.raw)
		if err != nil {
			return compiledRulepackSet{}, fmt.Errorf("%s: %w", file.path, err)
		}
		if err := addRuleIDs(seen, pack.Rules); err != nil {
			return compiledRulepackSet{}, err
		}
		rules = append(rules, pack.Rules...)
	}
	slices.SortFunc(rules, func(left, right compiledRule) int {
		return strings.Compare(left.ID, right.ID)
	})
	if len(rules) == 0 {
		return compiledRulepackSet{}, fmt.Errorf("rulepack v3 requires at least one rule")
	}

	pack := compiledPack{Version: 3, Kind: rulepackKindRules, Policy: policy, Rules: rules}

	return compiledRulepackSet{Version: 3, Policy: policy, Packs: []compiledPack{pack}}, nil
}

func compileV3Overlay(files []loadedRulepack, policy compiledPolicy) (compiledRulepackSet, error) {
	rules := make([]compiledRule, 0)
	seen := make(map[string]struct{})
	for _, file := range files {
		if err := validateV3File(file); err != nil {
			return compiledRulepackSet{}, err
		}
		if file.raw.Kind != rulepackKindRules {
			return compiledRulepackSet{}, fmt.Errorf("%s: v3 overlay must be rules-only", file.path)
		}
		pack, err := compileRulepack(&file.raw)
		if err != nil {
			return compiledRulepackSet{}, fmt.Errorf("%s: %w", file.path, err)
		}
		if err := addRuleIDs(seen, pack.Rules); err != nil {
			return compiledRulepackSet{}, err
		}
		rules = append(rules, pack.Rules...)
	}
	slices.SortFunc(rules, func(left, right compiledRule) int {
		return strings.Compare(left.ID, right.ID)
	})
	if len(rules) == 0 {
		return compiledRulepackSet{}, fmt.Errorf("rulepack v3 overlay requires at least one rule")
	}

	return compiledRulepackSet{
		Version: 3,
		Policy:  policy,
		Packs:   []compiledPack{{Version: 3, Kind: rulepackKindRules, Policy: policy, Rules: rules}},
	}, nil
}

func combineV3Sets(base, overlay compiledRulepackSet) (compiledRulepackSet, error) {
	if base.Version != 3 || overlay.Version != 3 {
		return compiledRulepackSet{}, fmt.Errorf("embedded baseline and overlay must both use rulepack v3")
	}
	rules := append(allRules(base.Packs), allRules(overlay.Packs)...)
	seen := make(map[string]struct{}, len(rules))
	if err := addRuleIDs(seen, rules); err != nil {
		return compiledRulepackSet{}, err
	}
	slices.SortFunc(rules, func(left, right compiledRule) int {
		return strings.Compare(left.ID, right.ID)
	})

	return compiledRulepackSet{
		Version: 3,
		Policy:  base.Policy,
		Packs:   []compiledPack{{Version: 3, Kind: rulepackKindRules, Policy: base.Policy, Rules: rules}},
	}, nil
}

func validateV3File(file loadedRulepack) error {
	if file.raw.Version != 3 {
		return fmt.Errorf("%s: v3 set cannot contain version %d", file.path, file.raw.Version)
	}
	if !file.hasKind || (file.raw.Kind != rulepackKindPolicy && file.raw.Kind != rulepackKindRules) {
		return fmt.Errorf("%s: rulepack v3 requires kind policy or rules", file.path)
	}
	if file.raw.Kind == rulepackKindPolicy {
		if !file.hasPolicy || file.hasRules {
			return fmt.Errorf("%s: policy file must contain policy and cannot contain rules", file.path)
		}

		return nil
	}
	if file.hasPolicy || !file.hasRules {
		return fmt.Errorf("%s: rules file must contain rules and cannot contain policy", file.path)
	}

	return nil
}

func validateV3Policy(policy rawPolicy) error {
	if !finiteFloat(policy.ReviewThreshold) || !finiteFloat(policy.BlockThreshold) ||
		policy.ReviewThreshold <= 0 || policy.BlockThreshold <= 0 || policy.ReviewThreshold > policy.BlockThreshold {
		return fmt.Errorf("invalid v3 review/block thresholds")
	}
	if policy.MinBlockFamilies <= 0 {
		return fmt.Errorf("min_block_families must be positive")
	}
	for name, value := range policy.SegmentMultipliers {
		if _, ok := parseSegment(name); !ok || !finiteFloat(value) || value <= 0 {
			return fmt.Errorf("invalid segment multiplier %q", name)
		}
	}
	for name, value := range policy.ViewMultipliers {
		if normalizeView(name) == "" || !finiteFloat(value) || value <= 0 {
			return fmt.Errorf("invalid view multiplier %q", name)
		}
	}

	return nil
}

func finiteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func addRuleIDs(seen map[string]struct{}, rules []compiledRule) error {
	for i := range rules {
		rule := &rules[i]
		if _, exists := seen[rule.ID]; exists {
			return fmt.Errorf("duplicate rule id %q", rule.ID)
		}
		seen[rule.ID] = struct{}{}
	}

	return nil
}

func allRules(packs []compiledPack) []compiledRule {
	var rules []compiledRule
	for _, pack := range packs {
		rules = append(rules, pack.Rules...)
	}

	return rules
}

func decodeRulepackFile(rulepackPath string, data []byte) (loadedRulepack, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return loadedRulepack{}, fmt.Errorf("parse yaml node: %w", err)
	}
	if err := lintRulepackNode(&root); err != nil {
		return loadedRulepack{}, fmt.Errorf("lint rulepack: %w", err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var raw rawRulepack
	if err := decoder.Decode(&raw); err != nil {
		return loadedRulepack{}, fmt.Errorf("parse rulepack: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return loadedRulepack{}, fmt.Errorf("multiple YAML documents are unsupported")
		}

		return loadedRulepack{}, fmt.Errorf("parse trailing document: %w", err)
	}

	document := mappingDocument(&root)
	raw.Kind = strings.ToLower(strings.TrimSpace(raw.Kind))

	return loadedRulepack{
		path:      rulepackPath,
		raw:       raw,
		hasKind:   findMappingValue(document, "kind") != nil,
		hasPolicy: findMappingValue(document, "policy") != nil,
		hasRules:  findMappingValue(document, "rules") != nil,
	}, nil
}

func mappingDocument(root *yaml.Node) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		return root.Content[0]
	}

	return root
}

func logRulepackSet(logger *slog.Logger, set compiledRulepackSet) {
	if logger == nil {
		return
	}
	logger.Info(
		"rulepack_set_loaded",
		slog.Int("version", set.Version),
		slog.Int("rule_count", len(allRules(set.Packs))),
		slog.String("policy_digest", set.Digest),
	)
}

func readRulepackFile(baseDir, rulepackPath string) ([]byte, error) {
	cleanBase := filepath.Clean(baseDir)
	cleanPath := filepath.Clean(rulepackPath)

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

func findRulepackFiles(dir string) []string {
	var files []string
	if err := filepath.WalkDir(dir, func(rulepackPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry == nil || entry.IsDir() {
			return nil
		}
		lower := strings.ToLower(entry.Name())
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
	if err := fs.WalkDir(fsys, root, func(rulepackPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry == nil || entry.IsDir() {
			return nil
		}
		lower := strings.ToLower(entry.Name())
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
