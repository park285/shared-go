package workercontract

import (
	"encoding/json/jsontext"
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

	for name, raw := range map[string]string{
		"missing key":   `{"poll_interval_ms":250,"max_attempts":3}`,
		"unknown key":   `{"poll_interval_ms":250,"max_attempts":3,"Untagged":true,"extra":1}`,
		"ignored key":   `{"poll_interval_ms":250,"max_attempts":3,"Untagged":true,"Ignored":1}`,
		"not an object": `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			var out sampleWorkerSettings

			err := DecodeWorkerSettings(profileWithSettings("inbox", raw), "inbox", &out)
			if err == nil {
				t.Fatal("invalid settings accepted")
			}
		})
	}
}

func TestDecodeWorkerSettingsSetValidatesRosterBeforeMutation(t *testing.T) {
	const alphaID = "alpha"

	type settings struct {
		Value int `json:"value"`
	}

	loaded := LoadedProfile{Profile: Profile{Workers: map[string]WorkerProfile{
		alphaID: {Settings: jsontext.Value(`{"value":11}`)},
		"zeta":  {Settings: jsontext.Value(`{"value":22}`)},
	}}}

	for _, tc := range []struct {
		name string
		ids  []string
	}{
		{"missing decoder", []string{alphaID}},
		{"extra decoder", []string{alphaID, "zeta", "extra"}},
		{"same size wrong decoder", []string{alphaID, "other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			destination := settings{Value: -1}
			destinations := make(map[string]any, len(tc.ids))

			for _, id := range tc.ids {
				destinations[id] = &destination
			}

			if err := DecodeWorkerSettingsSet(loaded, destinations); err == nil {
				t.Fatal("non-exact decoder roster accepted")
			}

			if destination.Value != -1 {
				t.Fatal("settings mutated before decoder roster validation")
			}
		})
	}

	var alpha, zeta settings

	if err := DecodeWorkerSettingsSet(loaded, map[string]any{"zeta": &zeta, alphaID: &alpha}); err != nil {
		t.Fatal(err)
	}

	if alpha.Value != 11 || zeta.Value != 22 {
		t.Fatalf("settings decoded to wrong workers: alpha=%d zeta=%d", alpha.Value, zeta.Value)
	}
}

func TestDecodeWorkerSettingsRejectsMissingWorkerAndBadDestination(t *testing.T) {
	loaded := profileWithSettings("inbox", `{}`)

	var settings struct{}

	if err := DecodeWorkerSettings(loaded, "outbox", &settings); err == nil {
		t.Fatalf("missing worker error = %v", err)
	}

	var byValue struct{}

	if err := DecodeWorkerSettings(loaded, "inbox", byValue); err == nil {
		t.Fatalf("value destination error = %v", err)
	}

	var embedded struct{ sampleWorkerSettings }

	if err := DecodeWorkerSettings(loaded, "inbox", &embedded); err == nil {
		t.Fatalf("embedded destination error = %v", err)
	}
}

func TestDecodeWorkerSettingsRejectsNullNonPointerFields(t *testing.T) {
	for _, tc := range []struct {
		name        string
		destination any
	}{
		{"integer", &struct {
			Value int `json:"value"`
		}{}},
		{"boolean", &struct {
			Value bool `json:"value"`
		}{}},
		{"string", &struct {
			Value string `json:"value"`
		}{}},
		{"struct", &struct {
			Value struct{} `json:"value"`
		}{}},
		{"slice", &struct {
			Value []int `json:"value"`
		}{}},
		{"map", &struct {
			Value map[string]int `json:"value"`
		}{}},
		{"array", &struct {
			Value [1]int `json:"value"`
		}{}},
		{"interface", &struct {
			Value any `json:"value"`
		}{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := DecodeWorkerSettings(profileWithSettings("inbox", `{"value": null}`), "inbox", tc.destination)
			if err == nil {
				t.Fatalf("null field error = %v", err)
			}
		})
	}
}

func TestDecodeWorkerSettingsValidatesNestedStructures(t *testing.T) {
	type inner struct {
		A int  `json:"a"`
		B bool `json:"b"`
	}

	type settings struct {
		Inner inner `json:"inner"`
	}

	type pointerSettings struct {
		Inner *inner `json:"inner"`
	}

	type sliceSettings struct {
		Items []inner `json:"items"`
	}

	type arraySettings struct {
		Items [1]inner `json:"items"`
	}

	type mapSettings struct {
		Items map[string]inner `json:"items"`
	}

	for _, tc := range []struct {
		name        string
		raw         string
		destination any
	}{
		{"missing nested key", `{"inner":{"a":1}}`, &settings{}},
		{"unknown nested key", `{"inner":{"a":1,"b":false,"extra":0}}`, &settings{}},
		{"case changed nested key", `{"inner":{"A":1,"b":false}}`, &settings{}},
		{"duplicate nested key", `{"inner":{"a":1,"a":2,"b":false}}`, &settings{}},
		{"null nested scalar", `{"inner":{"a":1,"b":null}}`, &settings{}},
		{"missing pointer key", `{}`, &pointerSettings{}},
		{"missing key through pointer", `{"inner":{"a":1}}`, &pointerSettings{}},
		{"missing slice element key", `{"items":[{"a":1,"b":true},{"a":2}]}`, &sliceSettings{}},
		{"null slice element", `{"items":[null]}`, &sliceSettings{}},
		{"missing array element key", `{"items":[{"b":true}]}`, &arraySettings{}},
		{"missing map value key", `{"items":{"first":{"a":1}}}`, &mapSettings{}},
		{"null map value", `{"items":{"first":null}}`, &mapSettings{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := DecodeWorkerSettings(profileWithSettings("inbox", tc.raw), "inbox", tc.destination)
			if err == nil {
				t.Fatal("invalid nested settings accepted")
			}
		})
	}

	var out struct {
		Inner *inner           `json:"inner"`
		Items []inner          `json:"items"`
		Array [1]inner         `json:"array"`
		Named map[string]inner `json:"named"`
	}

	raw := `{"inner":{"a":1,"b":false},"items":[{"a":2,"b":true}],"array":[{"a":3,"b":false}],"named":{"first":{"a":4,"b":true}}}`

	if err := DecodeWorkerSettings(profileWithSettings("inbox", raw), "inbox", &out); err != nil {
		t.Fatal(err)
	}

	if out.Inner == nil || out.Inner.A != 1 || out.Inner.B || len(out.Items) != 1 || out.Items[0] != (inner{A: 2, B: true}) ||
		out.Array[0] != (inner{A: 3}) || out.Named["first"] != (inner{A: 4, B: true}) {
		t.Fatalf("nested settings = %+v", out)
	}
}

func TestDecodeWorkerSettingsPreservesIntentionalPointerNull(t *testing.T) {
	type node struct {
		Value int   `json:"value"`
		Next  *node `json:"next"`
	}

	var out struct {
		PrimaryTimeoutMS *int64           `json:"primary_timeout_ms"`
		Inner            *node            `json:"inner"`
		Items            []*node          `json:"items"`
		Named            map[string]*node `json:"named"`
	}

	out.PrimaryTimeoutMS = new(int64(99))

	raw := `{"primary_timeout_ms":null,"inner":{"value":1,"next":null},"items":[null,{"value":2,"next":null}],"named":{"empty":null}}`

	if err := DecodeWorkerSettings(profileWithSettings("draw", raw), "draw", &out); err != nil {
		t.Fatal(err)
	}

	if out.PrimaryTimeoutMS != nil || out.Inner == nil || out.Inner.Value != 1 || out.Inner.Next != nil ||
		len(out.Items) != 2 || out.Items[0] != nil || out.Items[1] == nil || out.Items[1].Value != 2 || out.Named["empty"] != nil {
		t.Fatalf("pointer settings = %+v", out)
	}

	var timeout struct {
		PrimaryTimeoutMS *int64 `json:"primary_timeout_ms"`
	}

	if err := DecodeWorkerSettings(profileWithSettings("draw", `{"primary_timeout_ms":0}`), "draw", &timeout); err != nil {
		t.Fatal(err)
	}

	if timeout.PrimaryTimeoutMS == nil || *timeout.PrimaryTimeoutMS != 0 {
		t.Fatalf("primary timeout = %v, want non-nil zero", timeout.PrimaryTimeoutMS)
	}
}

func TestDecodeWorkerSettingsRejectsNilDestination(t *testing.T) {
	var typedNil *sampleWorkerSettings

	for _, destination := range []any{nil, typedNil} {
		if err := DecodeWorkerSettings(profileWithSettings("inbox", `{}`), "inbox", destination); err == nil {
			t.Fatalf("DecodeWorkerSettings(%T) accepted a nil destination", destination)
		}
	}
}
