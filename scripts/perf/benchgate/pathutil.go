package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func pyPathStr(s string) string {
	if s == "" {
		return "."
	}
	abs := strings.HasPrefix(s, "/")
	var out []string
	for seg := range strings.SplitSeq(s, "/") {
		if seg == "" || seg == "." {
			continue
		}
		out = append(out, seg)
	}
	joined := strings.Join(out, "/")
	if abs {
		return "/" + joined
	}
	if joined == "" {
		return "."
	}
	return joined
}

func isUnder(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func hasDotDot(p string) bool {
	return slices.Contains(strings.Split(p, string(filepath.Separator)), "..")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func resolveSymlinks(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
