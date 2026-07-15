package promptguard

import "github.com/park285/shared-go/pkg/internal/guardtext"

const maxBase64Candidates = 8

func decodedTextSegments(input string) ([]textSegment, guardtext.DecodeStatus) {
	result := guardtext.DecodeCandidatesWithContext(input)
	segments := make([]textSegment, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		segments = append(segments, textSegment{
			Kind:  segmentPlain,
			Views: normalizeViews(candidate),
		})
	}

	return segments, result.Status
}
