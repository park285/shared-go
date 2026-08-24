package llm

import (
	"errors"
	"reflect"
	"testing"
)

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
