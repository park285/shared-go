package promptguard

import "github.com/park285/shared-go/pkg/internal/guardtext"

const maxBase64Candidates = 8

func decodedTextSegments(input string) []textSegment {
	candidates := guardtext.DecodeCandidates(input)
	segments := make([]textSegment, 0, len(candidates))
	for _, candidate := range candidates {
		segments = append(segments, textSegment{
			Kind:  segmentPlain,
			Views: normalizeViews(candidate),
		})
	}

	return segments
}

func decodedBase64Segments(input string) []textSegment {
	return decodedTextSegments(input)
}
