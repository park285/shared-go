package llm

import (
	"errors"
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

func TestAdaptInstructionMessagesOpenAIPreservesMultipleCacheBreakpoints(t *testing.T) {
	t.Parallel()

	messages := []Message{
		{Role: roleSystem, Content: "invariant one", CacheBreakpoint: true},
		{Role: roleSystem, Content: "invariant two"},
		{Role: roleSystem, Content: "invariant three", CacheBreakpoint: true},
		{Role: roleDeveloper, Content: "persona-independent", CacheBreakpoint: true},
		{Role: roleDeveloper, Content: "persona-specific", CacheBreakpoint: true},
		{Role: roleDeveloper, Content: "runtime context"},
		{Role: roleUser, Content: testQuestion, CacheBreakpoint: true},
	}

	want := []Message{
		{Role: roleDeveloper, Content: "[APPLICATION INVARIANTS]\ninvariant one", CacheBreakpoint: true},
		{Role: roleDeveloper, Content: "invariant two\n\ninvariant three", CacheBreakpoint: true},
		{Role: roleDeveloper, Content: "[DEVELOPER INSTRUCTIONS]\npersona-independent", CacheBreakpoint: true},
		{Role: roleDeveloper, Content: "persona-specific", CacheBreakpoint: true},
		{Role: roleDeveloper, Content: "runtime context"},
		{Role: roleUser, Content: testQuestion, CacheBreakpoint: true},
	}

	got, err := AdaptInstructionMessages(messages, InstructionProfileOpenAI)
	if err != nil {
		t.Fatalf("AdaptInstructionMessages error = %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AdaptInstructionMessages = %#v, want %#v", got, want)
	}
}

func TestAdaptInstructionMessagesSingleProfilesOmitMissingSection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		messages []Message
		profile  InstructionProfile
		want     []Message
	}{
		{
			name:     "single developer invariant only",
			messages: []Message{{Role: roleSystem, Content: testInvariant}},
			profile:  InstructionProfileSingleDeveloper,
			want:     []Message{{Role: roleDeveloper, Content: "[APPLICATION INVARIANTS]\ninvariant"}},
		},
		{
			name:     "single developer developer only",
			messages: []Message{{Role: roleDeveloper, Content: roleDeveloper}},
			profile:  InstructionProfileSingleDeveloper,
			want:     []Message{{Role: roleDeveloper, Content: "[DEVELOPER INSTRUCTIONS]\ndeveloper"}},
		},
		{
			name:     "single system invariant only",
			messages: []Message{{Role: roleSystem, Content: testInvariant}},
			profile:  InstructionProfileSingleSystem,
			want:     []Message{{Role: roleSystem, Content: "[APPLICATION INVARIANTS]\ninvariant"}},
		},
		{
			name:     "single system developer only",
			messages: []Message{{Role: roleDeveloper, Content: roleDeveloper}},
			profile:  InstructionProfileSingleSystem,
			want:     []Message{{Role: roleSystem, Content: "[DEVELOPER INSTRUCTIONS]\ndeveloper"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := AdaptInstructionMessages(tt.messages, tt.profile)
			if err != nil {
				t.Fatalf("AdaptInstructionMessages error = %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("AdaptInstructionMessages = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAdaptInstructionMessagesOmitsEmptyLayers(t *testing.T) {
	t.Parallel()

	messages := []Message{
		{Role: roleSystem, Content: " \t\n", CacheBreakpoint: true},
		{Role: roleDeveloper, Content: "", CacheBreakpoint: true},
		{Role: roleUser, Content: testQuestion},
	}

	for _, profile := range []InstructionProfile{
		InstructionProfileOpenAI,
		InstructionProfileSingleDeveloper,
		InstructionProfileSingleSystem,
	} {
		got, err := AdaptInstructionMessages(messages, profile)
		if err != nil {
			t.Fatalf("AdaptInstructionMessages(%d) error = %v", profile, err)
		}

		want := []Message{{Role: roleUser, Content: testQuestion}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("AdaptInstructionMessages(%d) = %#v, want %#v", profile, got, want)
		}
	}
}

func TestAdaptInstructionMessagesRejectsInvalidSequence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		messages []Message
	}{
		{
			name: "invariant after developer",
			messages: []Message{
				{Role: roleDeveloper, Content: roleDeveloper},
				{Role: roleSystem, Content: testInvariant},
			},
		},
		{
			name: "invariant after user",
			messages: []Message{
				{Role: roleUser, Content: testQuestion},
				{Role: roleSystem, Content: testInvariant},
			},
		},
		{
			name: "developer after assistant",
			messages: []Message{
				{Role: roleAssistant, Content: testAnswer},
				{Role: roleDeveloper, Content: roleDeveloper},
			},
		},
		{
			name: "developer after unknown role",
			messages: []Message{
				{Role: testUnknown, Content: "serialized as user"},
				{Role: roleDeveloper, Content: roleDeveloper},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := AdaptInstructionMessages(tt.messages, InstructionProfileOpenAI)
			if !errors.Is(err, ErrInvalidInstructionSequence) {
				t.Fatalf("AdaptInstructionMessages error = %v, want ErrInvalidInstructionSequence", err)
			}

			if got != nil {
				t.Fatalf("AdaptInstructionMessages output = %#v, want nil", got)
			}
		})
	}
}

func TestAdaptInstructionMessagesRejectsInvalidProfile(t *testing.T) {
	t.Parallel()

	messages := []Message{
		{Role: roleDeveloper, Content: roleDeveloper},
		{Role: roleSystem, Content: "also an invalid sequence"},
	}
	got, err := AdaptInstructionMessages(messages, InstructionProfile(255))

	if !errors.Is(err, ErrInvalidInstructionProfile) {
		t.Fatalf("AdaptInstructionMessages error = %v, want ErrInvalidInstructionProfile", err)
	}

	if errors.Is(err, ErrInvalidInstructionSequence) {
		t.Fatalf("AdaptInstructionMessages error = %v, want profile validation first", err)
	}

	if got != nil {
		t.Fatalf("AdaptInstructionMessages output = %#v, want nil", got)
	}
}

func TestAdaptInstructionMessagesDoesNotMutateCallerMessages(t *testing.T) {
	t.Parallel()

	messages := []Message{
		{Role: " system ", Content: " invariant "},
		{Role: " DEVELOPER ", Content: " developer "},
		{Role: roleUser, Content: testQuestion},
	}
	wantInput := append([]Message(nil), messages...)

	for _, profile := range []InstructionProfile{
		InstructionProfileOpenAI,
		InstructionProfileSingleDeveloper,
		InstructionProfileSingleSystem,
	} {
		got, err := AdaptInstructionMessages(messages, profile)
		if err != nil {
			t.Fatalf("AdaptInstructionMessages(%d) error = %v", profile, err)
		}

		if !reflect.DeepEqual(messages, wantInput) {
			t.Fatalf("caller messages after profile %d = %#v, want %#v", profile, messages, wantInput)
		}

		got[len(got)-1].Content = "changed"

		if !reflect.DeepEqual(messages, wantInput) {
			t.Fatalf("caller messages aliased output for profile %d: %#v", profile, messages)
		}
	}
}
