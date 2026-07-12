package workerconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// RuntimeWorkerProfileEnvelope는 Iris diagnostics가 게시한 profile identity와 payload를 함께 보존한다.
type RuntimeWorkerProfileEnvelope struct {
	ProfileID      string
	ProfileVersion int
	ProducerHash   string
	Profile        IrisBotWebhookWorkerProfile
}

type runtimeWorkerProfilePipeline struct {
	ProfileEnabled *bool           `json:"profileEnabled"`
	ProfileVersion *int            `json:"profileVersion"`
	ProfileID      *string         `json:"profileId"`
	ProfileHash    *string         `json:"profileHash"`
	WorkerProfile  json.RawMessage `json:"workerProfile"`
}

type runtimeWorkerProfileDiagnostics struct {
	Workers struct {
		Webhook struct {
			WebhookPipeline runtimeWorkerProfilePipeline `json:"webhookPipeline"`
		} `json:"webhook"`
	} `json:"workers"`
}

// DecodeRuntimeWorkerProfileEnvelope는 Iris runtime diagnostics의 worker profile envelope를 해석한다.
func DecodeRuntimeWorkerProfileEnvelope(reader io.Reader) (RuntimeWorkerProfileEnvelope, error) {
	var diagnostics runtimeWorkerProfileDiagnostics
	if err := decodeSingleJSONValue(reader, &diagnostics); err != nil {
		return RuntimeWorkerProfileEnvelope{}, err
	}
	pipeline := diagnostics.Workers.Webhook.WebhookPipeline
	if err := validateRuntimeWorkerProfilePipeline(pipeline); err != nil {
		return RuntimeWorkerProfileEnvelope{}, err
	}
	profile, err := decodeRuntimeWorkerProfile(pipeline.WorkerProfile)
	if err != nil {
		return RuntimeWorkerProfileEnvelope{}, err
	}
	return buildRuntimeWorkerProfileEnvelope(pipeline, profile)
}

func decodeSingleJSONValue(reader io.Reader, value any) error {
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("diagnostics contains multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode diagnostics trailing JSON value: %w", err)
	}
	return nil
}

func validateRuntimeWorkerProfilePipeline(pipeline runtimeWorkerProfilePipeline) error {
	if pipeline.ProfileEnabled == nil {
		return errors.New("diagnostics workers.webhook.webhookPipeline.profileEnabled is missing")
	}
	if !*pipeline.ProfileEnabled {
		return ErrWorkerProfileDisabled
	}
	if pipeline.ProfileVersion == nil {
		return errors.New("diagnostics workers.webhook.webhookPipeline.profileVersion is missing")
	}
	if pipeline.ProfileID == nil {
		return errors.New("diagnostics workers.webhook.webhookPipeline.profileId is missing")
	}
	if pipeline.ProfileHash == nil {
		return errors.New("diagnostics workers.webhook.webhookPipeline.profileHash is missing")
	}
	if len(pipeline.WorkerProfile) == 0 || bytes.Equal(bytes.TrimSpace(pipeline.WorkerProfile), []byte("null")) {
		return errors.New("diagnostics workers.webhook.webhookPipeline.workerProfile is missing")
	}
	return nil
}

func decodeRuntimeWorkerProfile(payload json.RawMessage) (IrisBotWebhookWorkerProfile, error) {
	var identity struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(payload, &identity); err != nil {
		return IrisBotWebhookWorkerProfile{}, fmt.Errorf("decode diagnostics workerProfile identity: %w", err)
	}
	var wire wireIrisBotWebhookWorkerProfile
	profileDecoder := json.NewDecoder(bytes.NewReader(payload))
	if identity.Version == CurrentVersion {
		profileDecoder.DisallowUnknownFields()
	}
	if err := profileDecoder.Decode(&wire); err != nil {
		return IrisBotWebhookWorkerProfile{}, fmt.Errorf("decode diagnostics workerProfile: %w", err)
	}
	profile := fromWire(wire)
	if err := profile.Validate(); err != nil {
		return IrisBotWebhookWorkerProfile{}, err
	}
	return profile, nil
}

func buildRuntimeWorkerProfileEnvelope(
	pipeline runtimeWorkerProfilePipeline,
	profile IrisBotWebhookWorkerProfile,
) (RuntimeWorkerProfileEnvelope, error) {
	if profile.ProfileID != *pipeline.ProfileID {
		return RuntimeWorkerProfileEnvelope{}, errors.New("diagnostics workers.webhook.webhookPipeline.profileId does not match workerProfile.profile_id")
	}
	if profile.Version != *pipeline.ProfileVersion {
		return RuntimeWorkerProfileEnvelope{}, errors.New("diagnostics workers.webhook.webhookPipeline.profileVersion does not match workerProfile.version")
	}
	if profile.ProfileID == defaultProfileID || profile.ProfileID == "legacy-hardcoded" {
		return RuntimeWorkerProfileEnvelope{}, errors.New("diagnostics workerProfile.profile_id is not a production worker profile identity")
	}
	if profile.ProfileHash() != *pipeline.ProfileHash {
		return RuntimeWorkerProfileEnvelope{}, errors.New("diagnostics workers.webhook.webhookPipeline.profileHash does not match workerProfile")
	}
	return RuntimeWorkerProfileEnvelope{
		ProfileID:      *pipeline.ProfileID,
		ProfileVersion: *pipeline.ProfileVersion,
		ProducerHash:   *pipeline.ProfileHash,
		Profile:        profile,
	}, nil
}
