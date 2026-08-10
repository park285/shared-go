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
		{name: "unknown", model: "other-model", want: InstructionProfileOpenAI},
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
		{Role: "system", Content: "invariant one"},
		{Role: "system", Content: "invariant two"},
		{Role: "developer", Content: "developer one"},
		{Role: "developer", Content: "developer two"},
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "answer"},
		{Role: "unknown", Content: "unknown content"},
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
				{Role: "developer", Content: "[APPLICATION INVARIANTS]\ninvariant one\n\ninvariant two"},
				{Role: "developer", Content: "[DEVELOPER INSTRUCTIONS]\ndeveloper one\n\ndeveloper two"},
				{Role: "user", Content: "question"},
				{Role: "assistant", Content: "answer"},
				{Role: "unknown", Content: "unknown content"},
			},
		},
		{
			name:    "single developer",
			profile: InstructionProfileSingleDeveloper,
			want: []Message{
				{Role: "developer", Content: "[APPLICATION INVARIANTS]\ninvariant one\n\ninvariant two\n\n[DEVELOPER INSTRUCTIONS]\ndeveloper one\n\ndeveloper two"},
				{Role: "user", Content: "question"},
				{Role: "assistant", Content: "answer"},
				{Role: "unknown", Content: "unknown content"},
			},
		},
		{
			name:    "single system",
			profile: InstructionProfileSingleSystem,
			want: []Message{
				{Role: "system", Content: "[APPLICATION INVARIANTS]\ninvariant one\n\ninvariant two\n\n[DEVELOPER INSTRUCTIONS]\ndeveloper one\n\ndeveloper two"},
				{Role: "user", Content: "question"},
				{Role: "assistant", Content: "answer"},
				{Role: "unknown", Content: "unknown content"},
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
			messages := []Message{
				{Role: "system", Content: "invariant", CacheBreakpoint: tt.systemBreakpoint},
				{Role: "developer", Content: "developer", CacheBreakpoint: tt.developerBreakpoint},
				{Role: "user", Content: "question", CacheBreakpoint: true},
				{Role: "assistant", Content: "answer"},
				{Role: "unknown", Content: "unknown content", CacheBreakpoint: true},
			}

			for _, profile := range profiles {
				t.Run(profile.name, func(t *testing.T) {
					t.Parallel()
					got, err := AdaptInstructionMessages(messages, profile.profile)
					if err != nil {
						t.Fatalf("AdaptInstructionMessages error = %v", err)
					}

					var want []Message
					switch profile.profile {
					case InstructionProfileOpenAI:
						want = []Message{
							{Role: "developer", Content: "[APPLICATION INVARIANTS]\ninvariant", CacheBreakpoint: tt.systemBreakpoint},
							{Role: "developer", Content: "[DEVELOPER INSTRUCTIONS]\ndeveloper", CacheBreakpoint: tt.developerBreakpoint},
						}
					case InstructionProfileSingleDeveloper:
						want = []Message{{
							Role:            "developer",
							Content:         "[APPLICATION INVARIANTS]\ninvariant\n\n[DEVELOPER INSTRUCTIONS]\ndeveloper",
							CacheBreakpoint: tt.systemBreakpoint || tt.developerBreakpoint,
						}}
					case InstructionProfileSingleSystem:
						want = []Message{{
							Role:            "system",
							Content:         "[APPLICATION INVARIANTS]\ninvariant\n\n[DEVELOPER INSTRUCTIONS]\ndeveloper",
							CacheBreakpoint: tt.systemBreakpoint || tt.developerBreakpoint,
						}}
					}
					want = append(want,
						Message{Role: "user", Content: "question", CacheBreakpoint: true},
						Message{Role: "assistant", Content: "answer"},
						Message{Role: "unknown", Content: "unknown content", CacheBreakpoint: true},
					)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("AdaptInstructionMessages = %#v, want %#v", got, want)
					}
				})
			}
		})
	}
}

func TestAdaptInstructionMessagesOpenAIPreservesMultipleCacheBreakpoints(t *testing.T) {
	t.Parallel()

	messages := []Message{
		{Role: "system", Content: "invariant one", CacheBreakpoint: true},
		{Role: "system", Content: "invariant two"},
		{Role: "system", Content: "invariant three", CacheBreakpoint: true},
		{Role: "developer", Content: "persona-independent", CacheBreakpoint: true},
		{Role: "developer", Content: "persona-specific", CacheBreakpoint: true},
		{Role: "developer", Content: "runtime context"},
		{Role: "user", Content: "question", CacheBreakpoint: true},
	}

	want := []Message{
		{Role: "developer", Content: "[APPLICATION INVARIANTS]\ninvariant one", CacheBreakpoint: true},
		{Role: "developer", Content: "invariant two\n\ninvariant three", CacheBreakpoint: true},
		{Role: "developer", Content: "[DEVELOPER INSTRUCTIONS]\npersona-independent", CacheBreakpoint: true},
		{Role: "developer", Content: "persona-specific", CacheBreakpoint: true},
		{Role: "developer", Content: "runtime context"},
		{Role: "user", Content: "question", CacheBreakpoint: true},
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
			messages: []Message{{Role: "system", Content: "invariant"}},
			profile:  InstructionProfileSingleDeveloper,
			want:     []Message{{Role: "developer", Content: "[APPLICATION INVARIANTS]\ninvariant"}},
		},
		{
			name:     "single developer developer only",
			messages: []Message{{Role: "developer", Content: "developer"}},
			profile:  InstructionProfileSingleDeveloper,
			want:     []Message{{Role: "developer", Content: "[DEVELOPER INSTRUCTIONS]\ndeveloper"}},
		},
		{
			name:     "single system invariant only",
			messages: []Message{{Role: "system", Content: "invariant"}},
			profile:  InstructionProfileSingleSystem,
			want:     []Message{{Role: "system", Content: "[APPLICATION INVARIANTS]\ninvariant"}},
		},
		{
			name:     "single system developer only",
			messages: []Message{{Role: "developer", Content: "developer"}},
			profile:  InstructionProfileSingleSystem,
			want:     []Message{{Role: "system", Content: "[DEVELOPER INSTRUCTIONS]\ndeveloper"}},
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
		{Role: "system", Content: " \t\n", CacheBreakpoint: true},
		{Role: "developer", Content: "", CacheBreakpoint: true},
		{Role: "user", Content: "question"},
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
		want := []Message{{Role: "user", Content: "question"}}
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
				{Role: "developer", Content: "developer"},
				{Role: "system", Content: "invariant"},
			},
		},
		{
			name: "invariant after user",
			messages: []Message{
				{Role: "user", Content: "question"},
				{Role: "system", Content: "invariant"},
			},
		},
		{
			name: "developer after assistant",
			messages: []Message{
				{Role: "assistant", Content: "answer"},
				{Role: "developer", Content: "developer"},
			},
		},
		{
			name: "developer after unknown role",
			messages: []Message{
				{Role: "unknown", Content: "serialized as user"},
				{Role: "developer", Content: "developer"},
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
		{Role: "developer", Content: "developer"},
		{Role: "system", Content: "also an invalid sequence"},
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
		{Role: "user", Content: "question"},
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
