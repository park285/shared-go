package guardtext

import "testing"

func TestRuleCandidateExpansionSharesProtectedContextBudget(t *testing.T) {
	t.Parallel()

	decoder := contextDecoder{
		protectedWork: protectedDecodeWork{
			contextBytes: maxProtectedContextBytes,
		},
	}
	if decoder.ruleCandidateMayExpand("YQ==Wdub3Jl", "aWdub3Jl") {
		t.Fatal("expansion analysis exceeded the protected context budget")
	}
	if decoder.result.Status&DecodeByteLimit == 0 {
		t.Fatalf("status = %d, want DecodeByteLimit", decoder.result.Status)
	}
}

func TestRuleExpansionQueueIsBounded(t *testing.T) {
	t.Parallel()

	decoder := contextDecoder{
		queue:   make([]decodeQueueEntry, maxDecodeScans),
		visited: make(map[string]struct{}),
	}
	decoder.deferRuleCandidate(decodeQueueEntry{}, "ordinary readable candidate")
	if decoder.result.Status&DecodeScanLimit == 0 {
		t.Fatalf("status = %d, want DecodeScanLimit", decoder.result.Status)
	}
	if len(decoder.queue) != maxDecodeScans {
		t.Fatalf("queue length = %d, want %d", len(decoder.queue), maxDecodeScans)
	}
}
