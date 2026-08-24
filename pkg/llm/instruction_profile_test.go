package llm

import (
	"reflect"
	"testing"
)

func TestInstructionProfileForModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model string
		want  InstructionProfile
	}{
		{name: "grok", model: "grok-4.5", want: InstructionProfileSingleDeveloper},
		{name: "grok normalized", model: "  GrOk-4.5  ", want: InstructionProfileSingleDeveloper},
		{name: "gpt", model: "gpt-5.5", want: InstructionProfileOpenAI},
		{name: testUnknown, model: "other-model", want: InstructionProfileOpenAI},
		{name: "empty", model: "   ", want: InstructionProfileOpenAI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := InstructionProfileForModel(tt.model); got != tt.want {
				t.Fatalf("InstructionProfileForModel(%q) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}

func TestAdaptInstructionMessages(t *testing.T) {
	t.Parallel()

	messages := []Message{
		{Role: roleSystem, Content: "invariant one"},
		{Role: roleSystem, Content: "invariant two"},
		{Role: roleDeveloper, Content: "developer one"},
		{Role: roleDeveloper, Content: "developer two"},
		{Role: roleUser, Content: testQuestion},
		{Role: roleAssistant, Content: testAnswer},
		{Role: testUnknown, Content: testUnknownContent},
	}

	tests := []struct {
		name    string
		profile InstructionProfile
		want    []Message
	}{
		{
			name:    "openai",
			profile: InstructionProfileOpenAI,
			want: []Message{
				{Role: roleDeveloper, Content: "[APPLICATION INVARIANTS]\ninvariant one\n\ninvariant two"},
				{Role: roleDeveloper, Content: "[DEVELOPER INSTRUCTIONS]\ndeveloper one\n\ndeveloper two"},
				{Role: roleUser, Content: testQuestion},
				{Role: roleAssistant, Content: testAnswer},
				{Role: testUnknown, Content: testUnknownContent},
			},
		},
		{
			name:    "single developer",
			profile: InstructionProfileSingleDeveloper,
			want: []Message{
				{Role: roleDeveloper, Content: "[APPLICATION INVARIANTS]\ninvariant one\n\ninvariant two\n\n[DEVELOPER INSTRUCTIONS]\ndeveloper one\n\ndeveloper two"},
				{Role: roleUser, Content: testQuestion},
				{Role: roleAssistant, Content: testAnswer},
				{Role: testUnknown, Content: testUnknownContent},
			},
		},
		{
			name:    "single system",
			profile: InstructionProfileSingleSystem,
			want: []Message{
				{Role: roleSystem, Content: "[APPLICATION INVARIANTS]\ninvariant one\n\ninvariant two\n\n[DEVELOPER INSTRUCTIONS]\ndeveloper one\n\ndeveloper two"},
				{Role: roleUser, Content: testQuestion},
				{Role: roleAssistant, Content: testAnswer},
				{Role: testUnknown, Content: testUnknownContent},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := AdaptInstructionMessages(messages, tt.profile)
			if err != nil {
				t.Fatalf("AdaptInstructionMessages error = %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("AdaptInstructionMessages = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAdaptInstructionMessagesPreservesCacheBreakpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		systemBreakpoint    bool
		developerBreakpoint bool
	}{
		{name: "none"},
		{name: "system layer", systemBreakpoint: true},
		{name: "developer layer", developerBreakpoint: true},
		{name: "both layers", systemBreakpoint: true, developerBreakpoint: true},
	}

	profiles := []struct {
		name    string
		profile InstructionProfile
	}{
		{name: "openai", profile: InstructionProfileOpenAI},
		{name: "single developer", profile: InstructionProfileSingleDeveloper},
		{name: "single system", profile: InstructionProfileSingleSystem},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			messages := cacheBreakpointMessages(tt.systemBreakpoint, tt.developerBreakpoint)

			for _, profile := range profiles {
				t.Run(profile.name, func(t *testing.T) {
					t.Parallel()

					assertCacheBreakpointsPreserved(t, messages, profile.profile, tt.systemBreakpoint, tt.developerBreakpoint)
				})
			}
		})
	}
}

func cacheBreakpointMessages(systemBreakpoint, developerBreakpoint bool) []Message {
	return []Message{
		{Role: roleSystem, Content: testInvariant, CacheBreakpoint: systemBreakpoint},
		{Role: roleDeveloper, Content: roleDeveloper, CacheBreakpoint: developerBreakpoint},
		{Role: roleUser, Content: testQuestion, CacheBreakpoint: true},
		{Role: roleAssistant, Content: testAnswer},
		{Role: testUnknown, Content: testUnknownContent, CacheBreakpoint: true},
	}
}

func wantCacheBreakpointInstructions(profile InstructionProfile, systemBreakpoint, developerBreakpoint bool) []Message {
	switch profile {
	case InstructionProfileOpenAI:
		return []Message{
			{Role: roleDeveloper, Content: "[APPLICATION INVARIANTS]\ninvariant", CacheBreakpoint: systemBreakpoint},
			{Role: roleDeveloper, Content: "[DEVELOPER INSTRUCTIONS]\ndeveloper", CacheBreakpoint: developerBreakpoint},
		}
	case InstructionProfileSingleDeveloper:
		return []Message{{
			Role:            roleDeveloper,
			Content:         "[APPLICATION INVARIANTS]\ninvariant\n\n[DEVELOPER INSTRUCTIONS]\ndeveloper",
			CacheBreakpoint: systemBreakpoint || developerBreakpoint,
		}}
	case InstructionProfileSingleSystem:
		return []Message{{
			Role:            roleSystem,
			Content:         "[APPLICATION INVARIANTS]\ninvariant\n\n[DEVELOPER INSTRUCTIONS]\ndeveloper",
			CacheBreakpoint: systemBreakpoint || developerBreakpoint,
		}}
	}

	return nil
}

func assertCacheBreakpointsPreserved(t *testing.T, messages []Message, profile InstructionProfile, systemBreakpoint, developerBreakpoint bool) {
	t.Helper()

	got, err := AdaptInstructionMessages(messages, profile)
	if err != nil {
		t.Fatalf("AdaptInstructionMessages error = %v", err)
	}

	want := wantCacheBreakpointInstructions(profile, systemBreakpoint, developerBreakpoint)

	want = append(want,
		Message{Role: roleUser, Content: testQuestion, CacheBreakpoint: true},
		Message{Role: roleAssistant, Content: testAnswer},
		Message{Role: testUnknown, Content: testUnknownContent, CacheBreakpoint: true},
	)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AdaptInstructionMessages = %#v, want %#v", got, want)
	}
}
