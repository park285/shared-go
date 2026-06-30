package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type benchKey struct {
	pkg  string
	name string
}

type Sample struct {
	Ns     float64
	Bytes  *float64
	Allocs *float64
	File   string
}

type Results struct {
	Samples       map[benchKey][]Sample
	Race          bool
	Files         []string
	Count         *int
	Benchtime     *string
	CountFile     string
	BenchtimeFile string
}

var (
	benchLineRe  = regexp.MustCompile(`^(Benchmark\S*)\s+\d+\s+([0-9.]+)\s+ns/op(?:\s+([0-9.]+)\s+B/op)?(?:\s+([0-9.]+)\s+allocs/op)?`)
	countRe      = regexp.MustCompile(`(?:^|\s)-count=(\d+)(?:\s|$)`)
	benchtimeRe  = regexp.MustCompile(`(?:^|\s)-benchtime=([^\s)]+)`)
	nameSuffixRe = regexp.MustCompile(`-\d+$`)
	raceMarkerRe = regexp.MustCompile(`(^|[\s=])-race($|\s)`)
)

func hasRaceMarker(line string) bool {
	if raceMarkerRe.MatchString(line) {
		return true
	}
	return strings.Contains(strings.ToLower(line), "race detector")
}

func resultFiles(path string) []string {
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if !fi.IsDir() {
		return []string{path}
	}
	var files []string
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	// Python sorted(Path)와 동치인 path-parts 정렬: '/' 경계에서 단순 byte 정렬과 결과가 갈리므로 단순 정렬로 단순화하면 안 됨.
	slices.SortFunc(files, func(a, b string) int {
		return slices.Compare(strings.Split(a, "/"), strings.Split(b, "/"))
	})
	return files
}

func splitColon1(s string) string {
	_, after, found := strings.Cut(s, ":")
	if !found {
		return ""
	}
	return after
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func parseResults(path string) (*Results, error) {
	res := &Results{Samples: map[benchKey][]Sample{}}
	res.Files = resultFiles(path)
	for _, fp := range res.Files {
		currentPackage := ""
		currentPackageFromComment := false
		data, err := os.ReadFile(fp)
		if err != nil {
			return nil, perr("cannot read result file %s: %v", fp, err)
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			stripped := strings.TrimSpace(line)
			if hasRaceMarker(line) {
				res.Race = true
			}
			if strings.HasPrefix(stripped, "# package:") {
				currentPackage = strings.TrimSpace(splitColon1(stripped))
				currentPackageFromComment = true
				continue
			}
			if strings.HasPrefix(stripped, "pkg:") && !currentPackageFromComment {
				currentPackage = strings.TrimSpace(splitColon1(stripped))
				continue
			}
			if strings.HasPrefix(stripped, "# count:") {
				rawCount := strings.TrimSpace(splitColon1(stripped))
				if isAllDigits(rawCount) {
					c, _ := strconv.Atoi(rawCount)
					res.Count = &c
					res.CountFile = fp
				}
				continue
			}
			if strings.HasPrefix(stripped, "# benchtime:") {
				bt := strings.TrimSpace(splitColon1(stripped))
				res.Benchtime = &bt
				res.BenchtimeFile = fp
				continue
			}
			if strings.HasPrefix(stripped, "# command:") {
				command := splitColon1(stripped)
				if m := countRe.FindStringSubmatch(command); m != nil {
					c, _ := strconv.Atoi(m[1])
					res.Count = &c
					res.CountFile = fp
				}
				if m := benchtimeRe.FindStringSubmatch(command); m != nil {
					bt := m[1]
					res.Benchtime = &bt
					res.BenchtimeFile = fp
				}
			}
			m := benchLineRe.FindStringSubmatch(stripped)
			if m == nil {
				continue
			}
			name := nameSuffixRe.ReplaceAllString(m[1], "")
			ns, _ := strconv.ParseFloat(m[2], 64)
			s := Sample{Ns: ns, File: fp}
			if m[3] != "" {
				b, _ := strconv.ParseFloat(m[3], 64)
				s.Bytes = &b
			}
			if m[4] != "" {
				a, _ := strconv.ParseFloat(m[4], 64)
				s.Allocs = &a
			}
			key := benchKey{currentPackage, name}
			res.Samples[key] = append(res.Samples[key], s)
		}
	}
	return res, nil
}
