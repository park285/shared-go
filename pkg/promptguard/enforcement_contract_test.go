package promptguard

import (
	"reflect"
	"testing"
)

func TestGuardExposesSourceAwareCheckContract(t *testing.T) {
	t.Parallel()

	if _, ok := reflect.TypeFor[*Guard]().MethodByName("Check"); !ok {
		t.Fatal("Guard.Check is missing")
	}
	for _, removed := range []string{"Evaluate", "EnsureSafe", "EnsureSafeFrom"} {
		if _, ok := reflect.TypeFor[*Guard]().MethodByName(removed); ok {
			t.Fatalf("Guard still exposes removed compatibility method %s", removed)
		}
	}
	if _, ok := reflect.TypeFor[Config]().FieldByName("OnEvaluation"); !ok {
		t.Fatal("Config.OnEvaluation is missing")
	}
	if _, ok := reflect.TypeFor[Config]().FieldByName("OnReview"); ok {
		t.Fatal("Config still exposes removed review callback")
	}
	if _, ok := reflect.TypeFor[*Evaluation]().MethodByName("Malicious"); ok {
		t.Fatal("Evaluation still exposes removed block-only helper")
	}
}

func TestConfigDoesNotExposeRuntimeThresholdOverride(t *testing.T) {
	t.Parallel()

	if _, ok := reflect.TypeFor[Config]().FieldByName("Threshold"); ok {
		t.Fatal("Config still exposes removed Threshold compatibility field")
	}
}
