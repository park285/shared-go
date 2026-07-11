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

	var invariants []string
	var developers []string
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
			invariants = append(invariants, message.Content)
		case roleDeveloper:
			if strings.TrimSpace(message.Content) == "" {
				continue
			}
			if phase == instructionPhaseHistory {
				return nil, fmt.Errorf("%w: developer at index %d follows history", ErrInvalidInstructionSequence, i)
			}
			phase = instructionPhaseDeveloper
			developers = append(developers, message.Content)
		default:
			phase = instructionPhaseHistory
			history = append(history, message)
		}
	}

	adapted := make([]Message, 0, len(history)+2)
	switch profile {
	case InstructionProfileOpenAI:
		adapted = appendInstructionSection(adapted, roleDeveloper, applicationInvariantsLabel, invariants)
		adapted = appendInstructionSection(adapted, roleDeveloper, developerInstructionsLabel, developers)
	case InstructionProfileSingleDeveloper:
		adapted = appendFlattenedInstruction(adapted, roleDeveloper, invariants, developers)
	case InstructionProfileSingleSystem:
		adapted = appendFlattenedInstruction(adapted, roleSystem, invariants, developers)
	}
	return append(adapted, history...), nil
}

type instructionPhase uint8

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

func appendInstructionSection(messages []Message, role, label string, contents []string) []Message {
	if len(contents) == 0 {
		return messages
	}
	return append(messages, Message{
		Role:    role,
		Content: labeledInstruction(label, strings.Join(contents, "\n\n")),
	})
}

func appendFlattenedInstruction(messages []Message, role string, invariants, developers []string) []Message {
	sections := make([]string, 0, 2)
	if len(invariants) > 0 {
		sections = append(sections, labeledInstruction(applicationInvariantsLabel, strings.Join(invariants, "\n\n")))
	}
	if len(developers) > 0 {
		sections = append(sections, labeledInstruction(developerInstructionsLabel, strings.Join(developers, "\n\n")))
	}
	if len(sections) == 0 {
		return messages
	}
	return append(messages, Message{Role: role, Content: strings.Join(sections, "\n\n")})
}

func labeledInstruction(label, prompt string) string {
	return label + "\n" + prompt
}
