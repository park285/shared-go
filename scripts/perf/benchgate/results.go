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
	Samples map[benchKey][]Sample
	Files   []string
}

var (
	benchLineRe  = regexp.MustCompile(`^(Benchmark\S*)\s+\d+\s+([0-9.]+)\s+ns/op(?:\s+([0-9.]+)\s+B/op)?(?:\s+([0-9.]+)\s+allocs/op)?`)
	nameSuffixRe = regexp.MustCompile(`-\d+$`)
)

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

func parseResults(path string) (*Results, error) {
	res := &Results{Samples: map[benchKey][]Sample{}}
	res.Files = resultFiles(path)
	return parseResultsFiles(res, res.Files)
}

func parseResultsFromManifest(root string, files map[string]string) (*Results, error) {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, filepath.Join(root, path))
	}
	slices.Sort(paths)
	return parseResultsFiles(&Results{Samples: map[benchKey][]Sample{}, Files: paths}, paths)
}

func parseResultsFiles(res *Results, files []string) (*Results, error) {
	for _, fp := range files {
		currentPackage := ""
		currentPackageFromComment := false
		data, err := os.ReadFile(fp)
		if err != nil {
			return nil, perr("cannot read result file %s: %v", fp, err)
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			stripped := strings.TrimSpace(line)
			if strings.HasPrefix(stripped, "# package:") {
				currentPackage = strings.TrimSpace(splitColon1(stripped))
				currentPackageFromComment = true
				continue
			}
			if strings.HasPrefix(stripped, "pkg:") && !currentPackageFromComment {
				currentPackage = strings.TrimSpace(splitColon1(stripped))
				continue
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
