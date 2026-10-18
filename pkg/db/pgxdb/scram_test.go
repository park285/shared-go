package pgxdb

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSCRAMPasswordCompatibility(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		password   string
		postgres   string
		compatible bool
	}{
		{"empty", "", "", false},
		{"ascii", "ascii-password", "ascii-password", true},
		{"quotes", "quote'\\password", "quote'\\password", true},
		{"hangul", "비밀번호", "비밀번호", true},
		{"hangul normalization", "pass-\u1100\u1161", "pass-가", true},
		{"nonbreaking space", "a\u00a0b", "a b", true},
		{"ascii control fallback", "control\a", "control\a", true},
		{"private use fallback", "private-\ue000", "private-\ue000", true},
		{"prohibited before normalization", "a\u0340", "a\u0340", false},
		{"unassigned before normalization", "a\u1d2c", "a\u1d2c", true},
		{"fullwidth", "pass-Ｆ", "pass-F", false},
		{"soft hyphen", "soft\u00adhyphen", "softhyphen", false},
		{"zero width space mapping order", "a\u200bb", "a b", false},
		{"mapped empty fallback", "\u00ad", "\u00ad", true},
		{"bidi fallback", "a\u0627", "a\u0627", true},
		{"fallback must preserve normalization", "Ｆ\a", "Ｆ\a", true},
		{"long normalized fallback", "q" + strings.Repeat("\u0301", 35), "q" + strings.Repeat("\u0301", 35), true},
		{"long composed fallback", "á" + strings.Repeat("\u0301", 34), "á" + strings.Repeat("\u0301", 34), true},
		{"hangul combining boundary", "\u1100\u1161" + strings.Repeat("\u0301", 29), "가" + strings.Repeat("\u0301", 29), true},
		{"hangul stream-safe mismatch", "\u1100\u1161" + strings.Repeat("\u0301", 30), "가" + strings.Repeat("\u0301", 30), false},
		{"long composition mismatch", "a" + strings.Repeat("\u0301", 35), "á" + strings.Repeat("\u0301", 34), false},
		{"long ordering mismatch", "q" + strings.Repeat("\u0301", 31) + "\u0327", "q\u0327" + strings.Repeat("\u0301", 31), false},
		{"late composition mismatch", "A" + strings.Repeat("\u0327", 31) + "\u030a", "Å" + strings.Repeat("\u0327", 31), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := postgresSCRAMPassword(test.password); got != test.postgres {
				t.Fatal("PostgreSQL preparation did not match the expected bytes")
			}

			_, err := compatibleSCRAMPassword(test.password)
			if (err == nil) != test.compatible {
				t.Fatal("password compatibility decision did not match the native/client byte contract")
			}

			if err != nil && test.password != "" && strings.Contains(err.Error(), test.password) {
				t.Fatal("validation disclosed the input")
			}
		})
	}
}

func TestSCRAMVerifierRejectsWithoutDisclosingInput(t *testing.T) {
	t.Parallel()

	for _, password := range []string{"", "sentinel-Ｆ", "sentinel-soft\u00adhyphen", "sentinel\x00suffix", "sentinel-\xff", "sentinel-\xff\a", "sentinel-\xff\ue000"} {
		verifier, err := SCRAMVerifier(password)
		if err == nil || verifier != "" {
			t.Fatal("expected rejection without a verifier")
		}

		if password != "" && strings.Contains(err.Error(), password) {
			t.Fatal("rejection disclosed the input")
		}
	}
}

func TestSCRAMVerifierEncodingAndFreshSalt(t *testing.T) {
	t.Parallel()

	var previousSalt string

	for range 2 {
		verifier, err := SCRAMVerifier("synthetic-비밀번호")
		if err != nil {
			t.Fatal("compatible password rejected")
		}

		parts := strings.Split(verifier, "$")
		if len(parts) != 3 || parts[0] != "SCRAM-SHA-256" {
			t.Fatal("invalid verifier encoding")
		}

		iterations, saltText, ok := strings.Cut(parts[1], ":")
		if !ok || iterations != "4096" {
			t.Fatal("invalid iteration policy")
		}

		salt, err := base64.StdEncoding.DecodeString(saltText)
		if err != nil || len(salt) != 16 {
			t.Fatal("invalid salt")
		}

		if saltText == previousSalt {
			t.Fatal("SCRAM salt reused")
		}

		previousSalt = saltText

		stored, server, ok := strings.Cut(parts[2], ":")

		if !ok {
			t.Fatal("missing SCRAM keys")
		}

		for _, encoded := range []string{stored, server} {
			key, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || len(key) != 32 {
				t.Fatal("invalid SCRAM key encoding")
			}
		}
	}
}
