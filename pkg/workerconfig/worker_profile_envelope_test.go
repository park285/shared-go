package workerconfig_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/park285/shared-go/pkg/workerconfig"
)

func TestDecodeRuntimeWorkerProfileEnvelopeReturnsVerifiedIdentity(t *testing.T) {
	diagnostics := validRuntimeDiagnostics(t)

	envelope, err := workerconfig.DecodeRuntimeWorkerProfileEnvelope(strings.NewReader(diagnostics))
	if err != nil {
		t.Fatalf("DecodeRuntimeWorkerProfileEnvelope() error = %v", err)
	}
	if envelope.ProfileID != "prod-standard-2026-05-26" {
		t.Fatalf("ProfileID = %q", envelope.ProfileID)
	}
	if envelope.ProfileVersion != workerconfig.CurrentVersion {
		t.Fatalf("ProfileVersion = %d", envelope.ProfileVersion)
	}
	if envelope.ProducerHash != envelope.Profile.ProfileHash() {
		t.Fatalf("ProducerHash = %q, consumer hash = %q", envelope.ProducerHash, envelope.Profile.ProfileHash())
	}
}

func TestDecodeRuntimeWorkerProfileEnvelopeRejectsProducerHashMismatch(t *testing.T) {
	diagnostics := strings.Replace(
		validRuntimeDiagnostics(t),
		"48e6b84fe794daa2a349ebb77b6f9e8f1054e5bcfe336b7ab5fe2dbe1dcb8b1f",
		"5f4bb7659f48a6064e959f6985b0996d7f9cb9f9866d1c47bad98a416f6f6994",
		1,
	)

	_, err := workerconfig.DecodeRuntimeWorkerProfileEnvelope(strings.NewReader(diagnostics))
	if err == nil {
		t.Fatal("DecodeRuntimeWorkerProfileEnvelope() error = nil, want producer hash mismatch")
	}
	if !strings.Contains(err.Error(), "profileHash") {
		t.Fatalf("DecodeRuntimeWorkerProfileEnvelope() error = %v", err)
	}
}

func TestDecodeRuntimeWorkerProfileEnvelopeRejectsFlattenedProfileIDMismatch(t *testing.T) {
	diagnostics := strings.Replace(
		validRuntimeDiagnostics(t),
		`"profileId": "prod-standard-2026-05-26"`,
		`"profileId": "different-profile"`,
		1,
	)

	_, err := workerconfig.DecodeRuntimeWorkerProfileEnvelope(strings.NewReader(diagnostics))
	if err == nil {
		t.Fatal("DecodeRuntimeWorkerProfileEnvelope() error = nil, want profileId mismatch")
	}
	if !strings.Contains(err.Error(), "profileId") {
		t.Fatalf("DecodeRuntimeWorkerProfileEnvelope() error = %v", err)
	}
}

func TestDecodeRuntimeWorkerProfileEnvelopeRejectsFlattenedVersionMismatch(t *testing.T) {
	diagnostics := strings.Replace(
		validRuntimeDiagnostics(t),
		`"profileVersion": 1`,
		`"profileVersion": 2`,
		1,
	)

	_, err := workerconfig.DecodeRuntimeWorkerProfileEnvelope(strings.NewReader(diagnostics))
	if err == nil {
		t.Fatal("DecodeRuntimeWorkerProfileEnvelope() error = nil, want profileVersion mismatch")
	}
	if !strings.Contains(err.Error(), "profileVersion") {
		t.Fatalf("DecodeRuntimeWorkerProfileEnvelope() error = %v", err)
	}
}

func TestDecodeRuntimeWorkerProfileEnvelopeRejectsUnknownV1PayloadField(t *testing.T) {
	diagnostics := strings.Replace(
		validRuntimeDiagnostics(t),
		`"lane_workers": 32,`,
		`"lane_workers": 32, "unexpected": true,`,
		1,
	)

	_, err := workerconfig.DecodeRuntimeWorkerProfileEnvelope(strings.NewReader(diagnostics))
	if err == nil {
		t.Fatal("DecodeRuntimeWorkerProfileEnvelope() error = nil, want unknown v1 payload field error")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("DecodeRuntimeWorkerProfileEnvelope() error = %v", err)
	}
}

func TestDecodeRuntimeWorkerProfileEnvelopeRejectsTrailingJSON(t *testing.T) {
	_, err := workerconfig.DecodeRuntimeWorkerProfileEnvelope(strings.NewReader(validRuntimeDiagnostics(t) + `{}`))
	if err == nil || !strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("DecodeRuntimeWorkerProfileEnvelope() error = %v, want trailing JSON rejection", err)
	}
}

func TestDecodeRuntimeWorkerProfileEnvelopeRejectsFallbackIdentity(t *testing.T) {
	diagnostics := strings.ReplaceAll(
		validRuntimeDiagnostics(t),
		"prod-standard-2026-05-26",
		"default",
	)

	_, err := workerconfig.DecodeRuntimeWorkerProfileEnvelope(strings.NewReader(diagnostics))
	if err == nil {
		t.Fatal("DecodeRuntimeWorkerProfileEnvelope() error = nil, want fallback identity error")
	}
	if !strings.Contains(err.Error(), "production worker profile") {
		t.Fatalf("DecodeRuntimeWorkerProfileEnvelope() error = %v", err)
	}
}

func validRuntimeDiagnostics(t *testing.T) string {
	t.Helper()
	profile, err := os.ReadFile("testdata/worker-profile-v1-legacy.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return fmt.Sprintf(`{
		"workers": {"webhook": {"webhookPipeline": {
			"profileEnabled": true,
			"profileVersion": 1,
			"profileId": "prod-standard-2026-05-26",
			"profileHash": "48e6b84fe794daa2a349ebb77b6f9e8f1054e5bcfe336b7ab5fe2dbe1dcb8b1f",
			"workerProfile": %s
		}}}
	}`, profile)
}
