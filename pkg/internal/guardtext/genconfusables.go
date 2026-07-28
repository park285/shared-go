//go:build ignore

package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

const (
	unicodeVersion          = "17.0.0"
	confusablesSourceURL    = "https://www.unicode.org/Public/17.0.0/security/confusables.txt"
	confusablesSourceSHA256 = "091c7f82fc39ef208faf8f94d29c244de99254675e09de163160c810d13ef22a"
	unicodeDataBaselineURL  = "https://www.unicode.org/Public/15.0.0/ucd/UnicodeData.txt"
	unicodeDataBaselineSHA  = "806e9aed65037197f1ec85e12be6e8cd870fc5608b4de0fffd990f689f376a73"
	unicodeDataSourceURL    = "https://www.unicode.org/Public/17.0.0/ucd/UnicodeData.txt"
	unicodeDataSourceSHA256 = "2e1efc1dcb59c575eedf5ccae60f95229f706ee6d031835247d843c11d96470c"
	maxSourceBytes          = 4 << 20
)

type unicodeDataEntry struct {
	canonicalCombiningClass uint8
	canonicalDecomposition  []rune
}

func main() {
	confusablesSourcePath := flag.String("confusables-source", "testdata/confusables-17.0.0.txt.gz", "path to the pinned Unicode confusables source")
	unicodeDataBaselinePath := flag.String("unicode-data-baseline-source", "testdata/UnicodeData-15.0.0.txt.gz", "path to the pinned baseline Unicode character database source")
	unicodeDataSourcePath := flag.String("unicode-data-source", "testdata/UnicodeData-17.0.0.txt.gz", "path to the pinned Unicode character database source")
	outputPath := flag.String("output", "confusables_table_generated.go", "path to the generated Go table")
	check := flag.Bool("check", false, "verify that the generated output is current without writing it")
	flag.Parse()

	if err := run(*confusablesSourcePath, *unicodeDataBaselinePath, *unicodeDataSourcePath, *outputPath, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(confusablesSourcePath, unicodeDataBaselinePath, unicodeDataSourcePath, outputPath string, check bool) error {
	confusablesSource, err := loadSource(confusablesSourcePath, "Unicode confusables", confusablesSourceSHA256)
	if err != nil {
		return err
	}
	mappings, err := parseConfusables(confusablesSource)
	if err != nil {
		return err
	}
	unicodeDataBaselineSource, err := loadSource(unicodeDataBaselinePath, "baseline Unicode character database", unicodeDataBaselineSHA)
	if err != nil {
		return err
	}
	unicodeDataBaseline, err := parseUnicodeData(unicodeDataBaselineSource)
	if err != nil {
		return err
	}
	unicodeDataSource, err := loadSource(unicodeDataSourcePath, "Unicode character database", unicodeDataSourceSHA256)
	if err != nil {
		return err
	}
	unicodeData, err := parseUnicodeData(unicodeDataSource)
	if err != nil {
		return err
	}
	decompositionDelta, canonicalCombiningClassDelta, err := buildUnicode17NFDDelta(unicodeDataBaseline, unicodeData)
	if err != nil {
		return err
	}

	generated, err := render(mappings, decompositionDelta, canonicalCombiningClassDelta)
	if err != nil {
		return err
	}
	if check {
		current, err := os.ReadFile(outputPath)
		if err != nil {
			return fmt.Errorf("read generated confusables table: %w", err)
		}
		if !bytes.Equal(current, generated) {
			return fmt.Errorf("generated confusables table is stale")
		}
		return nil
	}
	if err := os.WriteFile(outputPath, generated, 0o600); err != nil {
		return fmt.Errorf("write generated confusables table: %w", err)
	}

	return nil
}

func loadSource(sourcePath, sourceName, sourceSHA256 string) ([]byte, error) {
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read %s source: %w", sourceName, err)
	}
	if strings.HasSuffix(sourcePath, ".gz") {
		compressedSource, err := gzip.NewReader(bytes.NewReader(source))
		if err != nil {
			return nil, fmt.Errorf("open compressed %s source: %w", sourceName, err)
		}
		source, err = io.ReadAll(io.LimitReader(compressedSource, maxSourceBytes+1))
		closeErr := compressedSource.Close()
		if err != nil {
			return nil, fmt.Errorf("decompress %s source: %w", sourceName, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close compressed %s source: %w", sourceName, closeErr)
		}
	}
	if len(source) > maxSourceBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", sourceName, maxSourceBytes)
	}

	digest := fmt.Sprintf("%x", sha256.Sum256(source))
	if digest != sourceSHA256 {
		return nil, fmt.Errorf("%s checksum = %s, want %s", sourceName, digest, sourceSHA256)
	}

	return source, nil
}

func parseConfusables(source []byte) (map[rune]string, error) {
	mappings := make(map[rune]string)
	scanner := bufio.NewScanner(bytes.NewReader(source))
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if line == "" {
			continue
		}

		fields := strings.Split(line, ";")
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid confusables line %q", scanner.Text())
		}

		sourceFields := strings.Fields(fields[0])
		targetFields := strings.Fields(fields[1])
		if len(sourceFields) != 1 || len(targetFields) == 0 {
			return nil, fmt.Errorf("invalid confusables mapping %q", scanner.Text())
		}

		sourceRune, err := parseRune(sourceFields[0])
		if err != nil {
			return nil, fmt.Errorf("parse source %q: %w", sourceFields[0], err)
		}
		if _, exists := mappings[sourceRune]; exists {
			return nil, fmt.Errorf("duplicate confusables source U+%04X", sourceRune)
		}

		var target strings.Builder
		for _, encoded := range targetFields {
			targetRune, err := parseRune(encoded)
			if err != nil {
				return nil, fmt.Errorf("parse target %q: %w", encoded, err)
			}
			target.WriteRune(targetRune)
		}
		mappings[sourceRune] = target.String()
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan Unicode confusables: %w", err)
	}
	if len(mappings) == 0 {
		return nil, fmt.Errorf("unicode confusables contained no mappings")
	}

	return mappings, nil
}

func parseUnicodeData(source []byte) (map[rune]unicodeDataEntry, error) {
	entries := make(map[rune]unicodeDataEntry)
	scanner := bufio.NewScanner(bytes.NewReader(source))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ";")
		if len(fields) != 15 {
			return nil, fmt.Errorf("invalid UnicodeData line %q", scanner.Text())
		}
		currentValue, err := strconv.ParseInt(fields[0], 16, 32)
		if err != nil {
			return nil, fmt.Errorf("parse UnicodeData scalar %q: %w", fields[0], err)
		}
		if currentValue >= 0xD800 && currentValue <= 0xDFFF {
			continue
		}
		if currentValue < 0 || currentValue > 0x10FFFF {
			return nil, fmt.Errorf("invalid UnicodeData scalar U+%X", currentValue)
		}
		current := rune(currentValue)
		canonicalCombiningClass, err := strconv.ParseUint(fields[3], 10, 8)
		if err != nil {
			return nil, fmt.Errorf("parse UnicodeData CCC %q: %w", fields[3], err)
		}

		entry := unicodeDataEntry{canonicalCombiningClass: uint8(canonicalCombiningClass)}
		if fields[5] != "" && !strings.HasPrefix(fields[5], "<") {
			for _, encoded := range strings.Fields(fields[5]) {
				decomposed, err := parseRune(encoded)
				if err != nil {
					return nil, fmt.Errorf("parse UnicodeData decomposition %q: %w", encoded, err)
				}
				entry.canonicalDecomposition = append(entry.canonicalDecomposition, decomposed)
			}
		}
		entries[current] = entry
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan UnicodeData: %w", err)
	}

	return entries, nil
}

func buildUnicode17NFDDelta(baselineEntries, entries map[rune]unicodeDataEntry) (map[rune]string, map[rune]uint8, error) {
	decompositionDelta := make(map[rune]string)
	canonicalCombiningClassDelta := make(map[rune]uint8)
	for current, entry := range entries {
		baselineEntry := baselineEntries[current]
		if !slices.Equal(baselineEntry.canonicalDecomposition, entry.canonicalDecomposition) {
			decompositionDelta[current] = string(decomposeUnicode17(current, entries, nil))
		}
		if baselineEntry.canonicalCombiningClass != entry.canonicalCombiningClass {
			canonicalCombiningClassDelta[current] = entry.canonicalCombiningClass
		}
	}
	if len(decompositionDelta) != 20 || len(canonicalCombiningClassDelta) != 46 {
		return nil, nil, fmt.Errorf(
			"Unicode 15 to 17 NFD delta = (%d decompositions, %d combining classes), want (20, 46)",
			len(decompositionDelta), len(canonicalCombiningClassDelta),
		)
	}

	return decompositionDelta, canonicalCombiningClassDelta, nil
}

func decomposeUnicode17(current rune, entries map[rune]unicodeDataEntry, active map[rune]bool) []rune {
	entry, ok := entries[current]
	if !ok || len(entry.canonicalDecomposition) == 0 {
		return []rune{current}
	}
	if active == nil {
		active = make(map[rune]bool)
	}
	if active[current] {
		panic(fmt.Sprintf("recursive Unicode canonical decomposition at U+%04X", current))
	}
	active[current] = true
	decomposed := make([]rune, 0, len(entry.canonicalDecomposition))
	for _, nested := range entry.canonicalDecomposition {
		decomposed = append(decomposed, decomposeUnicode17(nested, entries, active)...)
	}
	delete(active, current)

	return reorderUnicode17(decomposed, entries)
}

func reorderUnicode17(values []rune, entries map[rune]unicodeDataEntry) []rune {
	for current := 1; current < len(values); current++ {
		currentCCC := entries[values[current]].canonicalCombiningClass
		if currentCCC == 0 {
			continue
		}
		for previous := current; previous > 0; previous-- {
			previousCCC := entries[values[previous-1]].canonicalCombiningClass
			if previousCCC == 0 || previousCCC <= currentCCC {
				break
			}
			values[previous-1], values[previous] = values[previous], values[previous-1]
		}
	}

	return values
}

func parseRune(encoded string) (rune, error) {
	value, err := strconv.ParseInt(encoded, 16, 32)
	if err != nil {
		return 0, err
	}
	if value < 0 || value > 0x10FFFF || value >= 0xD800 && value <= 0xDFFF {
		return 0, fmt.Errorf("invalid Unicode scalar U+%X", value)
	}
	return rune(value), nil
}

func render(mappings, decompositionDelta map[rune]string, canonicalCombiningClassDelta map[rune]uint8) ([]byte, error) {
	keys := make([]rune, 0, len(mappings))
	for source := range mappings {
		keys = append(keys, source)
	}
	slices.Sort(keys)

	var output bytes.Buffer
	fmt.Fprintln(&output, "// Code generated by internal/genconfusables; DO NOT EDIT.")
	fmt.Fprintf(&output, "// Sources: %s, %s, and %s\n", confusablesSourceURL, unicodeDataBaselineURL, unicodeDataSourceURL)
	fmt.Fprintln(&output, "// Date: 2025-07-22, 05:49:37 GMT")
	fmt.Fprintln(&output, "// © 2025 Unicode, Inc.")
	fmt.Fprintln(&output, "// License: Unicode License V3; see THIRD_PARTY_NOTICES.md")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "package guardtext")
	fmt.Fprintln(&output)
	fmt.Fprintf(&output, "const confusablesUnicodeVersion = %q\n", unicodeVersion)
	fmt.Fprintf(&output, "const confusablesSourceSHA256 = %q\n", confusablesSourceSHA256)
	fmt.Fprintf(&output, "const unicodeDataBaselineSHA256 = %q\n", unicodeDataBaselineSHA)
	fmt.Fprintf(&output, "const unicodeDataSourceSHA256 = %q\n", unicodeDataSourceSHA256)
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "var confusablesMap = map[rune]string{")
	for _, source := range keys {
		fmt.Fprintf(&output, "0x%08X: %q,\n", source, mappings[source])
	}
	fmt.Fprintln(&output, "}")
	renderStringMap(&output, "unicode17CanonicalDecompositionDelta", decompositionDelta)
	renderUint8Map(&output, "unicode17CanonicalCombiningClassDelta", canonicalCombiningClassDelta)

	formatted, err := format.Source(output.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated table: %w", err)
	}

	return formatted, nil
}

func renderStringMap(output *bytes.Buffer, name string, mappings map[rune]string) {
	keys := make([]rune, 0, len(mappings))
	for source := range mappings {
		keys = append(keys, source)
	}
	slices.Sort(keys)
	fmt.Fprintf(output, "\nvar %s = map[rune]string{\n", name)
	for _, source := range keys {
		fmt.Fprintf(output, "0x%08X: %q,\n", source, mappings[source])
	}
	fmt.Fprintln(output, "}")
}

func renderUint8Map(output *bytes.Buffer, name string, mappings map[rune]uint8) {
	keys := make([]rune, 0, len(mappings))
	for source := range mappings {
		keys = append(keys, source)
	}
	slices.Sort(keys)
	fmt.Fprintf(output, "\nvar %s = map[rune]uint8{\n", name)
	for _, source := range keys {
		fmt.Fprintf(output, "0x%08X: %d,\n", source, mappings[source])
	}
	fmt.Fprintln(output, "}")
}
