package promptguard

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/park285/shared-go/v2/pkg/internal/guardtext"
)

func walkEvaluationSegments(
	text string,
	includeAggregate func(*aggregateTail, textSegment) bool,
	raw func(textSegment),
	aggregate func(textSegment),
) {
	tail := aggregateTail{}

	var first textSegment

	seenFirst, initialized := false, false

	visit := func(segment textSegment) {
		segment.piece = &segmentPiece{views: segment.Views}

		raw(segment)

		if !seenFirst {
			first = segment
			seenFirst = true

			return
		}

		if !initialized {
			tail.initialize(first)

			initialized = true
			first = textSegment{}
		}

		if includeAggregate == nil || includeAggregate(&tail, segment) {
			aggregate(tail.aggregateWith(segment))
		}

		tail.advance(segment)
	}

	for part := range strings.SplitSeq(text, guardBoundaryMarker) {
		walkTextSegments(part, visit)
	}
}

type aggregateTail struct {
	raw    rollingView
	norm   rollingView
	joined rollingView
	chunks []aggregateTailChunk
	// windowRawNorm은 현재 경계의 결합 원문 창을 정규화한 값이다. 사전 필터와 결합 세그먼트가 함께 쓰고
	// 경계를 넘길 때 비운다.
	windowRawNorm      string
	windowRawNormReady bool
	// literals는 현재 경계의 결합 세그먼트 view에서 찾은 필수 리터럴이다. 결합 세그먼트를 만들 때마다 비운다.
	literals segmentLiterals
}

type aggregateTailChunk struct {
	Kind        segmentKind
	RawRunes    int
	NormRunes   int
	JoinedRunes int
	// piece는 조각 전체의 view다. 조각이 tail에 남아 있는 동안 결합 경계 판정이 재사용한다.
	piece *segmentPiece
}

// segmentPiece는 한 평가 안에서 조각 하나의 view와 그로부터 다시 만든 view를 보관한다. 조각 평가와
// 결합 경계 판정이 같은 조각을 여러 번 보므로 가린 view와 raw 규칙용 정규화를 한 번만 만든다.
type segmentPiece struct {
	views        Views
	masked       []maskedPieceViews
	rawNorm      string
	rawNormReady bool
	literals     segmentLiterals
	// aggregateLiterals는 이 조각을 오른쪽에 둔 결합 세그먼트의 필수 리터럴 캐시다. 그 결합 세그먼트를 방문하는 동안만 유효하다.
	aggregateLiterals *segmentLiterals
}

type maskedPieceViews struct {
	key     string
	masked  string
	views   Views
	changed bool
}

// maskedViews는 규칙의 예외 문맥과 문장 경계를 가린 조각 view와 가린 범위가 있었는지를 돌려준다.
// 가림 설정이 같은 규칙은 같은 view를 쓰고, 설정이 달라도 가린 원문이 같으면(조각에 예외 문맥이 없어
// 문장 경계만 가린 경우) 정규화한 view를 다시 쓴다.
func (piece *segmentPiece) maskedViews(rule *compiledRule) (Views, bool) {
	for _, cached := range piece.masked {
		if cached.key == rule.MaskKey {
			return cached.views, cached.changed
		}
	}

	masked, changed := rule.maskExclusions(piece.views.Raw)
	views := piece.views

	if changed {
		index := slices.IndexFunc(piece.masked, func(cached maskedPieceViews) bool { return cached.changed && cached.masked == masked })
		if index >= 0 {
			views = piece.masked[index].views
		} else {
			views = normalizeViews(masked)
		}
	}

	piece.masked = append(piece.masked, maskedPieceViews{key: rule.MaskKey, masked: masked, views: views, changed: changed})

	return views, changed
}

// normalizedRaw는 raw 규칙이 원문에서 찾지 못한 근거를 다시 찾는 정규화 원문이다.
func (piece *segmentPiece) normalizedRaw() string {
	if !piece.rawNormReady {
		piece.rawNorm, piece.rawNormReady = guardtext.NormalizeView(piece.views.Raw), true
	}

	return piece.rawNorm
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
		piece:       segment.piece,
	})
}

// normalizedRawWindow는 현재 경계의 결합 원문 창을 정규화한다. 인자는 tail 원문 창과 오른쪽 조각
// 앞부분을 이은 값이어야 하며, 같은 경계에서는 처음 만든 값을 쓴다.
func (tail *aggregateTail) normalizedRawWindow(raw string) string {
	if !tail.windowRawNormReady {
		tail.windowRawNorm, tail.windowRawNormReady = normalizeText(raw), true
	}

	return tail.windowRawNorm
}

func (tail *aggregateTail) aggregateWith(right textSegment) textSegment {
	raw := tail.raw.withSuffix("", right.Views.Raw)
	rawNorm := tail.normalizedRawWindow(raw)
	norm := tail.norm.withSuffix(guardBoundaryMarker, right.Views.Norm)
	joined := tail.joined.withSuffix("", right.Views.Joined)

	tail.literals = segmentLiterals{}
	right.piece.aggregateLiterals = &tail.literals

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
		rawChunks: tail.chunks,
		piece:     right.piece,
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

func (view *rollingView) advance(separator, next string) (int, int) {
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

func (tail *aggregateTail) advance(segment textSegment) {
	tail.windowRawNorm, tail.windowRawNormReady = "", false

	rawAdded, rawTrimmed := tail.raw.advance("", segment.Views.Raw)
	normAdded, normTrimmed := tail.norm.advance(guardBoundaryMarker, segment.Views.Norm)
	joinedAdded, joinedTrimmed := tail.joined.advance("", segment.Views.Joined)

	tail.chunks = append(tail.chunks, aggregateTailChunk{
		Kind:        segment.Kind,
		RawRunes:    rawAdded,
		NormRunes:   normAdded,
		JoinedRunes: joinedAdded,
		piece:       segment.piece,
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
