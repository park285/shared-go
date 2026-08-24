package llm

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidInstructionSequence = errors.New("llm: invalid instruction sequence")
	ErrInvalidInstructionProfile  = errors.New("llm: invalid instruction profile")
)

type InstructionProfile uint8

const (
	InstructionProfileOpenAI InstructionProfile = iota
	InstructionProfileSingleDeveloper
	InstructionProfileSingleSystem
)

const (
	applicationInvariantsLabel = "[APPLICATION INVARIANTS]"
	developerInstructionsLabel = "[DEVELOPER INSTRUCTIONS]"
	roleAssistant              = "assistant"
	roleDeveloper              = "developer"
	roleSystem                 = "system"
	roleUser                   = "user"
)

func InstructionProfileForModel(model string) InstructionProfile {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "grok-") {
		return InstructionProfileSingleDeveloper
	}

	return InstructionProfileOpenAI
}

func AdaptInstructionMessages(messages []Message, profile InstructionProfile) ([]Message, error) {
	if !validInstructionProfile(profile) {
		return nil, fmt.Errorf("%w: %d", ErrInvalidInstructionProfile, profile)
	}

	var (
		invariants                []instructionPart
		developers                []instructionPart
		invariantsCacheBreakpoint bool
		developersCacheBreakpoint bool
	)

	history := make([]Message, 0, len(messages))
	phase := instructionPhaseInvariant

	for i, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		switch role {
		case roleSystem:
			if strings.TrimSpace(message.Content) == "" {
				continue
			}

			if phase == instructionPhaseDeveloper {
				return nil, fmt.Errorf("%w: invariant at index %d follows developer", ErrInvalidInstructionSequence, i)
			}

			if phase == instructionPhaseHistory {
				return nil, fmt.Errorf("%w: invariant at index %d follows history", ErrInvalidInstructionSequence, i)
			}

			invariants = append(invariants, instructionPart{content: message.Content, cacheBreakpoint: message.CacheBreakpoint})
			invariantsCacheBreakpoint = invariantsCacheBreakpoint || message.CacheBreakpoint
		case roleDeveloper:
			if strings.TrimSpace(message.Content) == "" {
				continue
			}

			if phase == instructionPhaseHistory {
				return nil, fmt.Errorf("%w: developer at index %d follows history", ErrInvalidInstructionSequence, i)
			}

			phase = instructionPhaseDeveloper

			developers = append(developers, instructionPart{content: message.Content, cacheBreakpoint: message.CacheBreakpoint})
			developersCacheBreakpoint = developersCacheBreakpoint || message.CacheBreakpoint
		default:
			phase = instructionPhaseHistory

			history = append(history, message)
		}
	}

	adapted := make([]Message, 0, len(history)+2)

	switch profile {
	case InstructionProfileOpenAI:
		adapted = appendInstructionSegments(adapted, roleDeveloper, applicationInvariantsLabel, invariants)
		adapted = appendInstructionSegments(adapted, roleDeveloper, developerInstructionsLabel, developers)
	case InstructionProfileSingleDeveloper:
		adapted = appendFlattenedInstruction(adapted, roleDeveloper, invariants, developers, invariantsCacheBreakpoint || developersCacheBreakpoint)
	case InstructionProfileSingleSystem:
		adapted = appendFlattenedInstruction(adapted, roleSystem, invariants, developers, invariantsCacheBreakpoint || developersCacheBreakpoint)
	}

	return append(adapted, history...), nil
}

type instructionPhase uint8

type instructionPart struct {
	content         string
	cacheBreakpoint bool
}

const (
	instructionPhaseInvariant instructionPhase = iota
	instructionPhaseDeveloper
	instructionPhaseHistory
)

func validInstructionProfile(profile InstructionProfile) bool {
	return profile == InstructionProfileOpenAI ||
		profile == InstructionProfileSingleDeveloper ||
		profile == InstructionProfileSingleSystem
}

func appendInstructionSegments(messages []Message, role, label string, contents []instructionPart) []Message {
	if len(contents) == 0 {
		return messages
	}

	segmentStart := 0
	firstSegment := true

	for i, content := range contents {
		if !content.cacheBreakpoint {
			continue
		}

		messages = appendInstructionSegment(messages, role, label, contents[segmentStart:i+1], firstSegment, true)
		segmentStart = i + 1
		firstSegment = false
	}

	if segmentStart < len(contents) {
		messages = appendInstructionSegment(messages, role, label, contents[segmentStart:], firstSegment, false)
	}

	return messages
}

func appendInstructionSegment(messages []Message, role, label string, contents []instructionPart, firstSegment, cacheBreakpoint bool) []Message {
	content := joinInstructionParts(contents)

	if firstSegment {
		content = labeledInstruction(label, content)
	}

	return append(messages, Message{Role: role, Content: content, CacheBreakpoint: cacheBreakpoint})
}

func appendFlattenedInstruction(messages []Message, role string, invariants, developers []instructionPart, cacheBreakpoint bool) []Message {
	sections := make([]string, 0, 2)

	if len(invariants) > 0 {
		sections = append(sections, labeledInstruction(applicationInvariantsLabel, joinInstructionParts(invariants)))
	}

	if len(developers) > 0 {
		sections = append(sections, labeledInstruction(developerInstructionsLabel, joinInstructionParts(developers)))
	}

	if len(sections) == 0 {
		return messages
	}

	return append(messages, Message{Role: role, Content: strings.Join(sections, "\n\n"), CacheBreakpoint: cacheBreakpoint})
}

func joinInstructionParts(contents []instructionPart) string {
	if len(contents) == 1 {
		return contents[0].content
	}

	totalLen := 2 * (len(contents) - 1)
	for _, content := range contents {
		totalLen += len(content.content)
	}

	var builder strings.Builder

	builder.Grow(totalLen)

	for i, content := range contents {
		if i > 0 {
			builder.WriteString("\n\n")
		}

		builder.WriteString(content.content)
	}

	return builder.String()
}

func labeledInstruction(label, prompt string) string {
	return label + "\n" + prompt
}
