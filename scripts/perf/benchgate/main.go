package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type policyError struct{ msg string }

func (e *policyError) Error() string { return e.msg }

func perr(format string, a ...any) *policyError {
	return &policyError{msg: fmt.Sprintf(format, a...)}
}

type cliArgs struct {
	baseline    string
	candidate   string
	policy      string
	gate        string
	gateID      string
	approvedSHA string
	count       *int
	benchtime   string
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	action, args, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 2
	}
	repoRoot, gerr := os.Getwd()
	if gerr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", gerr)
		return 2
	}
	repoName := repoNameForRoot(repoRoot)

	policy, pe := loadPolicy(args.policy)
	if pe != nil {
		return policyFail(pe)
	}
	if e := validatePolicy(policy, repoName); e != nil {
		return policyFail(e)
	}
	selected, e := selectedBenchmarks(policy, args.gate)
	if e != nil {
		return policyFail(e)
	}
	if action == "collect" {
		code, ce := collectResults(policy, selected, args, repoRoot, repoName)
		if ce != nil {
			return policyFail(ce)
		}
		return code
	}
	if action == "bootstrap-baseline" {
		code, ce := bootstrapBaseline(policy, selected, args, repoRoot, repoName)
		if ce != nil {
			return policyFail(ce)
		}
		return code
	}
	code, ce := checkResults(policy, selected, args, repoRoot, repoName)
	if ce != nil {
		return policyFail(ce)
	}
	return code
}

func repoNameForRoot(repoRoot string) string {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return filepath.Base(repoRoot)
	}
	commonDir := strings.TrimSpace(string(out))
	if commonDir == "" {
		return filepath.Base(repoRoot)
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repoRoot, commonDir)
	}
	if filepath.Base(commonDir) == ".git" {
		return filepath.Base(filepath.Dir(commonDir))
	}
	return filepath.Base(repoRoot)
}

func policyFail(e error) int {
	fmt.Fprintf(os.Stderr, "error: %s\n", e.Error())
	return 2
}

func parseArgs(argv []string) (string, *cliArgs, error) {
	action := "check"
	if len(argv) > 0 && (argv[0] == "check" || argv[0] == "collect" || argv[0] == "bootstrap-baseline") {
		action = argv[0]
		argv = argv[1:]
	}
	args := &cliArgs{
		baseline:  "artifacts/perf/baseline/main",
		candidate: "artifacts/perf/pr",
		policy:    "perf-budget.yaml",
		benchtime: DefaultBenchtime,
	}
	i := 0
	for i < len(argv) {
		arg := argv[i]
		if !strings.HasPrefix(arg, "--") {
			return "", nil, fmt.Errorf("unrecognized arguments: %s", arg)
		}
		name := arg
		val := ""
		hasEq := false
		if before, after, found := strings.Cut(arg, "="); found {
			name = before
			val = after
			hasEq = true
		}
		needVal := func() (string, error) {
			if hasEq {
				return val, nil
			}
			if i+1 >= len(argv) {
				return "", fmt.Errorf("argument %s: expected one argument", name)
			}
			i++
			return argv[i], nil
		}
		switch name {
		case "--baseline":
			v, e := needVal()
			if e != nil {
				return "", nil, e
			}
			args.baseline = v
		case "--candidate":
			v, e := needVal()
			if e != nil {
				return "", nil, e
			}
			args.candidate = v
		case "--policy":
			v, e := needVal()
			if e != nil {
				return "", nil, e
			}
			args.policy = v
		case "--gate":
			v, e := needVal()
			if e != nil {
				return "", nil, e
			}
			if !gatesSet[v] {
				return "", nil, fmt.Errorf("argument --gate: invalid choice: %q (choose from %s)", v, pyListRepr(sortedKeys(gatesSet)))
			}
			args.gate = v
		case "--gate-id":
			v, e := needVal()
			if e != nil {
				return "", nil, e
			}
			if v == "" {
				return "", nil, fmt.Errorf("argument --gate-id: must not be empty")
			}
			args.gateID = v
		case "--approved-sha":
			v, e := needVal()
			if e != nil {
				return "", nil, e
			}
			if !validHex(v, 40) {
				return "", nil, fmt.Errorf("argument --approved-sha: must be a 40-hex SHA")
			}
			args.approvedSHA = v
		case "--count":
			v, e := needVal()
			if e != nil {
				return "", nil, e
			}
			n, ce := strconv.Atoi(v)
			if ce != nil {
				return "", nil, fmt.Errorf("argument --count: invalid int value: %q", v)
			}
			args.count = &n
		case "--benchtime":
			v, e := needVal()
			if e != nil {
				return "", nil, e
			}
			args.benchtime = v
		default:
			return "", nil, fmt.Errorf("unrecognized arguments: %s", name)
		}
		i++
	}
	if args.count != nil && *args.count < 1 {
		return "", nil, fmt.Errorf("--count must be at least 1")
	}
	if args.gate == "" || args.gateID == "" {
		return "", nil, fmt.Errorf("action %s requires --gate and --gate-id", action)
	}
	if action == "bootstrap-baseline" && args.approvedSHA == "" {
		return "", nil, fmt.Errorf("action bootstrap-baseline requires --approved-sha")
	}
	return action, args, nil
}
