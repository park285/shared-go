package workercontract

import (
	"encoding/json/jsontext"
	"strings"
	"testing"
)

type sampleWorkerSettings struct {
	PollIntervalMS int64 `json:"poll_interval_ms"`
	MaxAttempts    int   `json:"max_attempts"`
	Ignored        int   `json:"-"`
	Untagged       bool
	hidden         int
}

func profileWithSettings(workerID, settings string) LoadedProfile {
	return LoadedProfile{Profile: Profile{Workers: map[string]WorkerProfile{
		workerID: {Settings: jsontext.Value(settings)},
	}}}
}

func TestDecodeWorkerSettingsRequiresExactTaggedKeys(t *testing.T) {
	var settings sampleWorkerSettings

	loaded := profileWithSettings("inbox", `{"poll_interval_ms":250,"max_attempts":3,"Untagged":true}`)
	if err := DecodeWorkerSettings(loaded, "inbox", &settings); err != nil {
		t.Fatalf("DecodeWorkerSettings() error = %v", err)
	}

	if settings.PollIntervalMS != 250 || settings.MaxAttempts != 3 || !settings.Untagged || settings.hidden != 0 {
		t.Fatalf("decoded settings = %+v", settings)
	}

	for name, tc := range map[string]struct {
		settings string
		want     string
	}{
		"missing key":   {settings: `{"poll_interval_ms":250,"max_attempts":3}`, want: "got keys [max_attempts poll_interval_ms], want [Untagged max_attempts poll_interval_ms]"},
		"unknown key":   {settings: `{"poll_interval_ms":250,"max_attempts":3,"Untagged":true,"extra":1}`, want: "worker settings"},
		"ignored key":   {settings: `{"poll_interval_ms":250,"max_attempts":3,"Untagged":true,"Ignored":1}`, want: "worker settings"},
		"not an object": {settings: `[]`, want: "object required"},
	} {
		t.Run(name, func(t *testing.T) {
			var out sampleWorkerSettings

			err := DecodeWorkerSettings(profileWithSettings("inbox", tc.settings), "inbox", &out)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("DecodeWorkerSettings() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestDecodeWorkerSettingsRejectsMissingWorkerAndBadDestination(t *testing.T) {
	loaded := profileWithSettings("inbox", `{}`)

	var settings struct{}

	if err := DecodeWorkerSettings(loaded, "outbox", &settings); err == nil || !strings.Contains(err.Error(), "worker is missing") {
		t.Fatalf("missing worker error = %v", err)
	}

	var byValue struct{}

	if err := DecodeWorkerSettings(loaded, "inbox", byValue); err == nil || !strings.Contains(err.Error(), "struct pointer") {
		t.Fatalf("value destination error = %v", err)
	}

	var embedded struct{ sampleWorkerSettings }

	if err := DecodeWorkerSettings(loaded, "inbox", &embedded); err == nil || !strings.Contains(err.Error(), "embed") {
		t.Fatalf("embedded destination error = %v", err)
	}
}
