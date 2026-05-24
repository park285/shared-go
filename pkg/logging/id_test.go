package logging

import (
	"regexp"
	"strings"
	"testing"
)

func TestNewIDFormat(t *testing.T) {
	got := NewID("job")
	matched, err := regexp.MatchString(`^job_[0-9]+_[0-9a-f]{12}$`, got)
	if err != nil {
		t.Fatalf("compile NewID regex: %v", err)
	}
	if !matched {
		t.Fatalf("NewID() = %q, want <prefix>_<unixMillis>_<hex>", got)
	}
}

func TestNewIDUsesSanitizedPrefix(t *testing.T) {
	input := "__Video.Job-01__"
	got := NewID(input)
	matches := regexp.MustCompile(`^(.+)_([0-9]+)_([0-9a-f]{12})$`).FindStringSubmatch(got)
	if matches == nil {
		t.Fatalf("NewID(%q) = %q, want sanitized prefix, timestamp, and hex suffix", input, got)
	}
	if matches[1] != sanitizeIDPrefix(input) {
		t.Fatalf("NewID prefix = %q, want %q", matches[1], sanitizeIDPrefix(input))
	}
}

func TestSanitizeIDPrefix(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "uppercase lowercased", in: "BatchJob", want: "batchjob"},
		{name: "spaces and specials removed while separators normalize", in: " Foo Bar/@Baz-QuX._9 ", want: "foobarbaz_qux__9"},
		{name: "empty falls back", in: "", want: "id"},
		{name: "blank falls back", in: " \t\n ", want: "id"},
		{name: "all disallowed falls back", in: " /@#$%^&*() ", want: "id"},
		{name: "trims surrounding underscores", in: "__Job.Name__", want: "job_name"},
		{name: "keeps alphanumeric", in: "Job123", want: "job123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeIDPrefix(tt.in); got != tt.want {
				t.Fatalf("sanitizeIDPrefix(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIDPrefixRuneClassifiers(t *testing.T) {
	alphaNumericCases := []struct {
		name string
		in   rune
		want bool
	}{
		{name: "lowercase", in: 'a', want: true},
		{name: "digit", in: '7', want: true},
		{name: "uppercase rejected", in: 'A', want: false},
		{name: "separator rejected", in: '-', want: false},
	}

	for _, tt := range alphaNumericCases {
		t.Run("alpha numeric "+tt.name, func(t *testing.T) {
			if got := isIDPrefixAlphaNumeric(tt.in); got != tt.want {
				t.Fatalf("isIDPrefixAlphaNumeric(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}

	separatorCases := []struct {
		name string
		in   rune
		want bool
	}{
		{name: "dash", in: '-', want: true},
		{name: "underscore", in: '_', want: true},
		{name: "dot", in: '.', want: true},
		{name: "space rejected", in: ' ', want: false},
		{name: "letter rejected", in: 'a', want: false},
	}

	for _, tt := range separatorCases {
		t.Run("separator "+tt.name, func(t *testing.T) {
			if got := isIDPrefixSeparator(tt.in); got != tt.want {
				t.Fatalf("isIDPrefixSeparator(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizedIDPrefixRune(t *testing.T) {
	tests := []struct {
		name string
		in   rune
		want rune
		ok   bool
	}{
		{name: "alphanumeric", in: 'x', want: 'x', ok: true},
		{name: "separator", in: '.', want: '_', ok: true},
		{name: "disallowed", in: '/', want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := sanitizedIDPrefixRune(tt.in)
			if ok != tt.ok {
				t.Fatalf("sanitizedIDPrefixRune(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("sanitizedIDPrefixRune(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewIDHasThreeSuffixParts(t *testing.T) {
	parts := strings.Split(NewID("job"), "_")
	if len(parts) != 3 {
		t.Fatalf("NewID split parts = %v, want prefix, timestamp, hex", parts)
	}
}
