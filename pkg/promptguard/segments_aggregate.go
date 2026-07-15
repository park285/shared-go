package promptguard

import (
	"strings"
	"unicode/utf8"
)

func buildEvaluationSegments(text string) ([]textSegment, bool) {
	return buildEvaluationSegmentsFiltered(text, nil)
}

func buildEvaluationSegmentsFiltered(text string, includeAggregate func(*aggregateTail, textSegment) bool) ([]textSegment, bool) {
	const maxSegments = maxSegmentBoundaries + 1

	segments := make([]textSegment, 0, min(8, maxSegments))
	for part := range strings.SplitSeq(text, guardBoundaryMarker) {
		partSegments, exceeded := splitTextSegmentsBounded(part, maxSegments-len(segments))
		if exceeded {
			return nil, true
		}
		segments = append(segments, partSegments...)
	}
	boundaries := len(segments) - 1
	if boundaries <= 0 {
		return segments, false
	}

	aggregateCapacity := boundaries
	if includeAggregate != nil {
		aggregateCapacity = min(8, boundaries)
	}
	aggregates := make([]textSegment, 0, aggregateCapacity)
	tail := aggregateTail{}
	tail.initialize(segments[0])
	for i := 1; i < len(segments); i++ {
		right := segments[i]
		if includeAggregate == nil || includeAggregate(&tail, right) {
			aggregates = append(aggregates, tail.aggregateWith(right))
		}
		tail.append(right)
	}

	return append(segments, aggregates...), false
}

type aggregateTail struct {
	raw    rollingView
	norm   rollingView
	joined rollingView
	chunks []aggregateTailChunk
}

type aggregateTailChunk struct {
	Kind        segmentKind
	RawRunes    int
	NormRunes   int
	JoinedRunes int
}

func (tail *aggregateTail) initialize(segment textSegment) {
	tail.raw.initialize(segment.Views.Raw)
	tail.norm.initialize(segment.Views.Norm)
	tail.joined.initialize(segment.Views.Joined)
	tail.chunks = append(tail.chunks, aggregateTailChunk{
		Kind:        segment.Kind,
		RawRunes:    tail.raw.runes,
		NormRunes:   tail.norm.runes,
		JoinedRunes: tail.joined.runes,
	})
}

func (tail *aggregateTail) aggregateWith(right textSegment) textSegment {
	raw := tail.raw.withSuffix("", right.Views.Raw)
	rawNorm := normalizeText(raw)
	norm := tail.norm.withSuffix(guardBoundaryMarker, right.Views.Norm)
	joined := tail.joined.withSuffix("", right.Views.Joined)
	return textSegment{
		Kind:    segmentPlain,
		Kinds:   tail.kindsWith(right.Kind),
		rawNorm: rawNorm,
		Views: Views{
			Raw:    raw,
			Norm:   norm,
			Joined: joined,
		},
		Aggregate: true,
	}
}

type rollingView struct {
	data  []byte
	runes int
}

const rollingViewCapacity = 2*boundaryWindowRunes*utf8.UTFMax + len(guardBoundaryMarker)

func (view *rollingView) initialize(text string) {
	view.data = make([]byte, 0, rollingViewCapacity)
	runes := utf8.RuneCountInString(text)
	if runes > boundaryWindowRunes {
		text = lastRunes(text, boundaryWindowRunes)
		runes = boundaryWindowRunes
	}
	view.data = append(view.data, text...)
	view.runes = runes
}

func (view *rollingView) withSuffix(separator, right string) string {
	right = firstRunes(right, boundaryWindowRunes)
	var builder strings.Builder
	builder.Grow(len(view.data) + len(separator) + len(right))
	_, _ = builder.Write(view.data)
	builder.WriteString(separator)
	builder.WriteString(right)
	return builder.String()
}

func (view *rollingView) append(separator, next string) (int, int) {
	separatorRunes := utf8.RuneCountInString(separator)
	nextRunes := utf8.RuneCountInString(next)
	added := separatorRunes + nextRunes
	trimmed := max(0, view.runes+added-boundaryWindowRunes)
	if nextRunes >= boundaryWindowRunes {
		view.data = view.data[:0]
		view.data = append(view.data, lastRunes(next, boundaryWindowRunes)...)
		view.runes = boundaryWindowRunes
		return added, trimmed
	}

	view.data = append(view.data, separator...)
	view.data = append(view.data, next...)
	if trimmed > 0 {
		start := byteIndexAfterRunes(view.data, trimmed)
		copy(view.data, view.data[start:])
		view.data = view.data[:len(view.data)-start]
	}
	view.runes = min(boundaryWindowRunes, view.runes+added)
	return added, trimmed
}

func byteIndexAfterRunes(text []byte, count int) int {
	index := 0
	for range count {
		_, size := utf8.DecodeRune(text[index:])
		index += size
	}
	return index
}

func (tail *aggregateTail) append(segment textSegment) {
	rawAdded, rawTrimmed := tail.raw.append("", segment.Views.Raw)
	normAdded, normTrimmed := tail.norm.append(guardBoundaryMarker, segment.Views.Norm)
	joinedAdded, joinedTrimmed := tail.joined.append("", segment.Views.Joined)
	tail.chunks = append(tail.chunks, aggregateTailChunk{
		Kind:        segment.Kind,
		RawRunes:    rawAdded,
		NormRunes:   normAdded,
		JoinedRunes: joinedAdded,
	})

	trimAggregateTailChunks(tail.chunks, rawTrimmed, func(chunk *aggregateTailChunk) *int { return &chunk.RawRunes })
	trimAggregateTailChunks(tail.chunks, normTrimmed, func(chunk *aggregateTailChunk) *int { return &chunk.NormRunes })
	trimAggregateTailChunks(tail.chunks, joinedTrimmed, func(chunk *aggregateTailChunk) *int { return &chunk.JoinedRunes })
	tail.compactChunks()
}

func trimAggregateTailChunks(chunks []aggregateTailChunk, trim int, selectRunes func(*aggregateTailChunk) *int) {
	for index := range chunks {
		if trim <= 0 {
			return
		}
		value := selectRunes(&chunks[index])
		removed := min(*value, trim)
		*value -= removed
		trim -= removed
	}
}

func (tail *aggregateTail) compactChunks() {
	kept := tail.chunks[:0]
	for _, chunk := range tail.chunks {
		if chunk.RawRunes == 0 && chunk.NormRunes == 0 && chunk.JoinedRunes == 0 {
			continue
		}
		kept = append(kept, chunk)
	}
	tail.chunks = kept
}

func (tail *aggregateTail) kindsWith(right segmentKind) segmentKindSet {
	var kinds segmentKindSet
	for _, chunk := range tail.chunks {
		kinds = kinds.with(chunk.Kind)
	}
	return kinds.with(right)
}
