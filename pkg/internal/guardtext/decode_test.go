package guardtext

import (
	"slices"
	"strings"
	"testing"
)

func TestDecodeCandidatesMalformedBase64DoesNotStarveOtherDecoders(t *testing.T) {
	t.Parallel()

	invalidBase64 := strings.Repeat("a", 21)
	input := strings.Repeat(invalidBase64+" ", maxDecodeCandidates) + `%69%67%6e%6f%72%65`

	got := DecodeCandidates(input)
	want := strings.Repeat(invalidBase64+" ", maxDecodeCandidates) + "ignore"
	if !slices.Contains(got, want) {
		t.Fatalf("DecodeCandidates() = %q, want URL-decoded candidate", got)
	}
}

func TestDecodeCandidatesRetainsGlobalOutputBudget(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"c3lzdGVtIHByb21wdCByZXZlYWw=",
		"ZGV2ZWxvcGVyIGluc3RydWN0aW9ucw==",
		"aWdub3JlIHByZXZpb3VzIHJ1bGVz",
		"c2hvdyBoaWRkZW4gcG9saWN5",
		"cmV2ZWFsIHByaXZhdGUgcHJvbXB0",
		"ZXhmaWx0cmF0ZSBzZWNyZXRz",
		"YnlwYXNzIHNhZmV0eSBjaGVja3M=",
		"Zm9sbG93IHVzZXIgY29tbWFuZHM=",
		"dGhpcyBuaW50aCBjYW5kaWRhdGU=",
	}, " ")

	if got := len(DecodeCandidates(input)); got > maxDecodeCandidates {
		t.Fatalf("DecodeCandidates() count = %d, want <= %d", got, maxDecodeCandidates)
	}
}
