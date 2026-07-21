package guardtext

import "testing"

func TestRuleCandidateExpansionBoundsWork(t *testing.T) {
	t.Parallel()

	decoder := ruleContextDecoder{expansionBytes: maxRuleExpansionBytes}
	decoder.beginRuleExpansion("YQ==Wdub3Jl")
	if decoder.ruleCandidateMayExpand("YQ==Wdub3Jl", "aWdub3Jl") {
		t.Fatal("expansion analysis exceeded its byte budget")
	}
	if decoder.result.Status&DecodeByteLimit == 0 {
		t.Fatalf("status = %d, want DecodeByteLimit", decoder.result.Status)
	}
}

func TestRuleCandidateExpansionChargesSourceOnce(t *testing.T) {
	t.Parallel()

	decoder := ruleContextDecoder{}
	decoder.beginRuleExpansion("source")
	if !decoder.consumeRuleExpansionWork(3) || !decoder.consumeRuleExpansionWork(4) {
		t.Fatalf("status = %d, want complete work accounting", decoder.result.Status)
	}
	if want := len("source") + 3 + 4; decoder.expansionBytes != want {
		t.Fatalf("expansion bytes = %d, want %d", decoder.expansionBytes, want)
	}
}

func TestRuleExpansionQueueIsBounded(t *testing.T) {
	t.Parallel()

	decoder := ruleContextDecoder{
		contextDecoder: contextDecoder{
			queue:   make([]decodeQueueEntry, maxDecodeScans),
			visited: make(map[string]struct{}),
		},
	}
	decoder.deferRuleCandidate(decodeQueueEntry{}, "ordinary readable candidate")
	if decoder.result.Status&DecodeScanLimit == 0 {
		t.Fatalf("status = %d, want DecodeScanLimit", decoder.result.Status)
	}
	if len(decoder.queue) != maxDecodeScans {
		t.Fatalf("queue length = %d, want %d", len(decoder.queue), maxDecodeScans)
	}
}
