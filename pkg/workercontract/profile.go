package workercontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
)

const (
	ContractVersion       = 1
	MaxProfileBytes       = 64 * 1024
	MaxConfiguredWorkers  = 4096
	MaxQueueCapacityItems = 1_048_576
	MaxFixedDurationMS    = 86_400_000
)

var (
	profileIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	workerIDPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// DurationMode는 실행 시간 또는 queue age 정책의 표현 방식을 고정한다.
type DurationMode string

const (
	DurationModeFixed  DurationMode = "fixed"
	DurationModePerJob DurationMode = "per_job"
	DurationModeNone   DurationMode = "none"
)

// CapacityMode는 queue가 bounded인지 명시한다.
type CapacityMode string

const (
	CapacityModeBounded   CapacityMode = "bounded"
	CapacityModeUnbounded CapacityMode = "unbounded"
)

// DurationPolicy는 fixed 또는 per-job duration을 표현한다.
type DurationPolicy struct {
	Mode         DurationMode `json:"mode"`
	Milliseconds *int64       `json:"milliseconds"`
}

// CapacityPolicy는 bounded/unbounded queue 용량을 표현한다.
type CapacityPolicy struct {
	Mode  CapacityMode `json:"mode"`
	Items *int64       `json:"items"`
}

// ExecutorProfile은 profile이 소유하는 executor 설정이다.
type ExecutorProfile struct {
	Enabled           bool           `json:"enabled"`
	ConfiguredWorkers int            `json:"configured_workers"`
	AttemptTimeout    DurationPolicy `json:"attempt_timeout"`
}

// QueueProfile은 canonical queue의 공통 정책이다.
type QueueProfile struct {
	Capacity CapacityPolicy `json:"capacity"`
	MaxAge   DurationPolicy `json:"max_age"`
}

// WorkerProfile은 공통 정책과 service-owned settings 원문을 보존한다.
type WorkerProfile struct {
	Executor ExecutorProfile `json:"executor"`
	Queue    QueueProfile    `json:"queue"`
	Settings json.RawMessage `json:"settings"`
}

// Profile은 Stack Worker Profile v1의 공통 표현이다.
type Profile struct {
	ContractVersion int                      `json:"contract_version"`
	Service         string                   `json:"service"`
	Role            string                   `json:"role"`
	ProfileID       string                   `json:"profile_id"`
	Workers         map[string]WorkerProfile `json:"workers"`
}

// Identity는 binary가 컴파일 시점에 소유하는 service, role, worker set이다.
type Identity struct {
	Service   string
	Role      string
	WorkerIDs []string
}

var knownWorkerSets = map[string][]string{
	"iris/runtime":               {"reply_delivery", "webhook_delivery"},
	"chatbotgo/bot":              {"command", "compaction", "draw", "summary", "webhook_inbox"},
	"twentyq/bot":                {"pending_queue", "player_registration", "reply_outbox", "webhook_inbox"},
	"hololive/api":               {"bot_reply_outbox", "bot_webhook_inbox", "source_observation"},
	"hololive/alarm-worker":      {"alarm_dispatch", "notification_delivery", "youtube_delivery"},
	"hololive/youtube-collector": {"collection"},
}

// KnownIdentity는 v1 registry에 고정된 binary identity를 반환한다.
func KnownIdentity(service, role string) (Identity, error) {
	workerIDs, ok := knownWorkerSets[service+"/"+role]
	if !ok {
		return Identity{}, fmt.Errorf("worker profile identity: unsupported service/role %q/%q", service, role)
	}
	return Identity{Service: service, Role: role, WorkerIDs: slices.Clone(workerIDs)}, nil
}

// LoadedProfile은 검증된 profile과 exact-byte identity다.
type LoadedProfile struct {
	Profile Profile
	Hash    string
	path    string
}

// Path는 file checker가 같은 source를 다시 확인할 때만 사용한다.
func (p LoadedProfile) Path() string { return p.path }

// DecodeSettings는 service-owned settings를 unknown field와 trailing JSON 없이 해석한다.
func DecodeSettings(raw json.RawMessage, destination any) error {
	if destination == nil {
		return errors.New("worker settings: nil destination")
	}
	if err := validateJSONDocument(raw); err != nil {
		return fmt.Errorf("worker settings: %w", err)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("worker settings: object required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("worker settings: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return fmt.Errorf("worker settings: %w", err)
	}
	return nil
}

func decodeProfile(raw []byte, identity Identity) (Profile, error) {
	if err := validateJSONDocument(raw); err != nil {
		return Profile{}, err
	}
	top, err := decodeExactObject(raw, "profile", "contract_version", "service", "role", "profile_id", "workers")
	if err != nil {
		return Profile{}, err
	}
	var profile Profile
	if err := decodeField(top, "contract_version", &profile.ContractVersion); err != nil {
		return Profile{}, err
	}
	if err := decodeField(top, "service", &profile.Service); err != nil {
		return Profile{}, err
	}
	if err := decodeField(top, "role", &profile.Role); err != nil {
		return Profile{}, err
	}
	if err := decodeField(top, "profile_id", &profile.ProfileID); err != nil {
		return Profile{}, err
	}
	workerRaw := map[string]json.RawMessage{}
	if err := decodeField(top, "workers", &workerRaw); err != nil {
		return Profile{}, err
	}
	profile.Workers = make(map[string]WorkerProfile, len(workerRaw))
	for workerID, encoded := range workerRaw {
		worker, decodeErr := decodeWorker(encoded, workerID)
		if decodeErr != nil {
			return Profile{}, decodeErr
		}
		profile.Workers[workerID] = worker
	}
	if err := validateProfile(profile, identity); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func decodeWorker(raw json.RawMessage, workerID string) (WorkerProfile, error) {
	fields, fieldsErr := decodeExactObject(raw, "workers."+workerID, "executor", "queue", "settings")
	if fieldsErr != nil {
		return WorkerProfile{}, fieldsErr
	}
	executorFields, executorErr := decodeExactObject(fields["executor"], "workers."+workerID+".executor", "enabled", "configured_workers", "attempt_timeout")
	if executorErr != nil {
		return WorkerProfile{}, executorErr
	}
	queueFields, queueErr := decodeExactObject(fields["queue"], "workers."+workerID+".queue", "capacity", "max_age")
	if queueErr != nil {
		return WorkerProfile{}, queueErr
	}
	var worker WorkerProfile
	if bytes.Equal(bytes.TrimSpace(executorFields["enabled"]), []byte("null")) {
		return WorkerProfile{}, fmt.Errorf("workers.%s.executor.enabled: boolean required", workerID)
	}
	if err := decodeField(executorFields, "enabled", &worker.Executor.Enabled); err != nil {
		return WorkerProfile{}, err
	}
	if err := decodeField(executorFields, "configured_workers", &worker.Executor.ConfiguredWorkers); err != nil {
		return WorkerProfile{}, err
	}
	attemptTimeout, err := decodeDuration(executorFields["attempt_timeout"], "workers."+workerID+".executor.attempt_timeout")
	if err != nil {
		return WorkerProfile{}, err
	}
	worker.Executor.AttemptTimeout = attemptTimeout
	capacity, err := decodeCapacity(queueFields["capacity"], "workers."+workerID+".queue.capacity")
	if err != nil {
		return WorkerProfile{}, err
	}
	worker.Queue.Capacity = capacity
	maxAge, err := decodeDuration(queueFields["max_age"], "workers."+workerID+".queue.max_age")
	if err != nil {
		return WorkerProfile{}, err
	}
	worker.Queue.MaxAge = maxAge
	settings := bytes.TrimSpace(fields["settings"])
	if len(settings) == 0 || settings[0] != '{' {
		return WorkerProfile{}, fmt.Errorf("workers.%s.settings: object required", workerID)
	}
	worker.Settings = slices.Clone(fields["settings"])
	return worker, nil
}

func decodeDuration(raw json.RawMessage, field string) (DurationPolicy, error) {
	fields, err := decodeExactObject(raw, field, "mode", "milliseconds")
	if err != nil {
		return DurationPolicy{}, err
	}
	var policy DurationPolicy
	if err := decodeField(fields, "mode", &policy.Mode); err != nil {
		return DurationPolicy{}, err
	}
	if !bytes.Equal(bytes.TrimSpace(fields["milliseconds"]), []byte("null")) {
		var milliseconds int64
		if err := decodeField(fields, "milliseconds", &milliseconds); err != nil {
			return DurationPolicy{}, err
		}
		policy.Milliseconds = &milliseconds
	}
	return policy, nil
}

func decodeCapacity(raw json.RawMessage, field string) (CapacityPolicy, error) {
	fields, err := decodeExactObject(raw, field, "mode", "items")
	if err != nil {
		return CapacityPolicy{}, err
	}
	var policy CapacityPolicy
	if err := decodeField(fields, "mode", &policy.Mode); err != nil {
		return CapacityPolicy{}, err
	}
	if !bytes.Equal(bytes.TrimSpace(fields["items"]), []byte("null")) {
		var items int64
		if err := decodeField(fields, "items", &items); err != nil {
			return CapacityPolicy{}, err
		}
		policy.Items = &items
	}
	return policy, nil
}

func decodeExactObject(raw json.RawMessage, field string, names ...string) (map[string]json.RawMessage, error) {
	value := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%s: object required", field)
	}
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
		if _, ok := value[name]; !ok {
			return nil, fmt.Errorf("%s.%s: required", field, name)
		}
	}
	for name := range value {
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("%s.%s: unknown field", field, name)
		}
	}
	return value, nil
}

func decodeField(fields map[string]json.RawMessage, name string, destination any) error {
	if err := json.Unmarshal(fields[name], destination); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func validateProfile(profile Profile, identity Identity) error {
	if profile.ContractVersion != ContractVersion {
		return fmt.Errorf("contract_version: got %d, want %d", profile.ContractVersion, ContractVersion)
	}
	if profile.Service != identity.Service || profile.Role != identity.Role {
		return fmt.Errorf("profile identity: got %s/%s, want %s/%s", profile.Service, profile.Role, identity.Service, identity.Role)
	}
	if !profileIDPattern.MatchString(profile.ProfileID) {
		return errors.New("profile_id: non-canonical value")
	}
	actual := make([]string, 0, len(profile.Workers))
	for workerID, worker := range profile.Workers {
		if !workerIDPattern.MatchString(workerID) {
			return fmt.Errorf("worker id %q: non-canonical value", workerID)
		}
		actual = append(actual, workerID)
		if err := validateWorker(workerID, worker); err != nil {
			return err
		}
	}
	slices.Sort(actual)
	expected := slices.Clone(identity.WorkerIDs)
	slices.Sort(expected)
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("workers: got %v, want %v", actual, expected)
	}
	return nil
}

func validateWorker(workerID string, worker WorkerProfile) error {
	prefix := "workers." + workerID
	if worker.Executor.ConfiguredWorkers < 1 || worker.Executor.ConfiguredWorkers > MaxConfiguredWorkers {
		return fmt.Errorf("%s.executor.configured_workers: out of range", prefix)
	}
	if err := validateDuration(worker.Executor.AttemptTimeout, false, prefix+".executor.attempt_timeout"); err != nil {
		return err
	}
	switch worker.Queue.Capacity.Mode {
	case CapacityModeBounded:
		if worker.Queue.Capacity.Items == nil || *worker.Queue.Capacity.Items < 1 || *worker.Queue.Capacity.Items > MaxQueueCapacityItems {
			return fmt.Errorf("%s.queue.capacity.items: out of range", prefix)
		}
	case CapacityModeUnbounded:
		if worker.Queue.Capacity.Items != nil {
			return fmt.Errorf("%s.queue.capacity.items: must be null", prefix)
		}
	default:
		return fmt.Errorf("%s.queue.capacity.mode: invalid", prefix)
	}
	return validateDuration(worker.Queue.MaxAge, true, prefix+".queue.max_age")
}

func validateDuration(policy DurationPolicy, allowNone bool, field string) error {
	switch policy.Mode {
	case DurationModeFixed:
		if policy.Milliseconds == nil || *policy.Milliseconds < 1 || *policy.Milliseconds > MaxFixedDurationMS {
			return fmt.Errorf("%s.milliseconds: out of range", field)
		}
	case DurationModePerJob:
		if policy.Milliseconds != nil {
			return fmt.Errorf("%s.milliseconds: must be null", field)
		}
	case DurationModeNone:
		if !allowNone {
			return fmt.Errorf("%s.mode: none is not allowed", field)
		}
		if policy.Milliseconds != nil {
			return fmt.Errorf("%s.milliseconds: must be null", field)
		}
	default:
		return fmt.Errorf("%s.mode: invalid", field)
	}
	return nil
}

func validateJSONDocument(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, tokenErr := decoder.Token()
			if tokenErr != nil {
				return tokenErr
			}
			key, keyOK := keyToken.(string)
			if !keyOK {
				return errors.New("JSON object key must be a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			if valueErr := consumeJSONValue(decoder); valueErr != nil {
				return valueErr
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if valueErr := consumeJSONValue(decoder); valueErr != nil {
				return valueErr
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("trailing JSON value")
}
