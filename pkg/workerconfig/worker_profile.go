package workerconfig

// 이 패키지는 Iris worker-profile 도메인 계약을 인코딩한다.
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CurrentVersion = 1
)

const (
	defaultProfileID = "default"

	defaultDeliveryLaneWorkers             = 32
	defaultDeliveryLaneQueueCapacity       = defaultDeliveryLaneWorkers * 4
	defaultDeliveryMaxGlobalInFlight       = 32
	defaultDeliveryMaxPerEndpointInFlight  = 8
	defaultDeliveryMaxDrainPerTick         = 128
	defaultDeliveryMaxAttempts             = 6
	defaultDeliveryRequestTimeout          = 125 * time.Second
	defaultDeliveryLaneIdleTimeout         = 750 * time.Millisecond
	deliveryRetryWaitCeiling               = 30 * time.Second
	defaultDeliveryBreakerFailureThreshold = uint32(5)
	defaultDeliveryBreakerCooldown         = 30 * time.Second

	defaultReceiveWorkers        = 16
	defaultReceiveQueueSize      = 1000
	defaultReceiveEnqueueTimeout = 50 * time.Millisecond
	defaultReceiveHandlerTimeout = 120 * time.Second
	defaultReceiveMaxBodyBytes   = 64 << 10
	defaultReceiveDedupTTL       = 16 * time.Minute
	defaultReceiveDedupTimeout   = 200 * time.Millisecond

	defaultBotPoolWorkers   = 10
	defaultBotPoolQueueSize = 100

	defaultMinQueuePerEndpointMultiplier = 4
	deliveryRequestTimeoutOverhang       = 5 * time.Second

	maxWorkers          = 4096
	maxQueueCapacity    = 1 << 20
	maxInFlight         = 1 << 16
	maxDrainPerTick     = 1 << 20
	maxAttempts         = 100
	maxQueueMultiplier  = 1024
	maxReceiveBodyBytes = 1 << 30
	maxDuration         = time.Hour
)

var ErrWorkerProfileDisabled = errors.New("iris bot webhook worker profile is disabled")

type IrisBotWebhookWorkerProfile struct {
	Version    int                                   `json:"version"`
	ProfileID  string                                `json:"profile_id"`
	Delivery   irisWebhookDeliveryWorkerProfile      `json:"delivery"`
	Receive    botWebhookReceiveWorkerProfile        `json:"receive"`
	BotPool    botPoolWorkerProfile                  `json:"bot_pool"`
	Validation irisBotWebhookWorkerProfileValidation `json:"validation"`
}

type irisWebhookDeliveryWorkerProfile struct {
	LaneWorkers             int           `json:"-"`
	LaneQueueCapacity       int           `json:"-"`
	MaxGlobalInFlight       int           `json:"-"`
	MaxPerEndpointInFlight  int           `json:"-"`
	MaxDrainPerTick         int           `json:"-"`
	MaxAttempts             int           `json:"-"`
	RequestTimeout          time.Duration `json:"-"`
	LaneIdleTimeout         time.Duration `json:"-"`
	BreakerFailureThreshold uint32        `json:"-"`
	BreakerCooldown         time.Duration `json:"-"`
}

type botWebhookReceiveWorkerProfile struct {
	Workers        int           `json:"-"`
	QueueSize      int           `json:"-"`
	EnqueueTimeout time.Duration `json:"-"`
	HandlerTimeout time.Duration `json:"-"`
	MaxBodyBytes   int64         `json:"-"`
	DedupTTL       time.Duration `json:"-"`
	DedupTimeout   time.Duration `json:"-"`
}

type botPoolWorkerProfile struct {
	Workers   int `json:"-"`
	QueueSize int `json:"-"`
}

type irisBotWebhookWorkerProfileValidation struct {
	MinQueuePerEndpointMultiplier          int  `json:"min_queue_per_endpoint_multiplier"`
	RequireReceiveCapacityForEndpointBurst bool `json:"require_receive_capacity_for_endpoint_burst"`
}

type wireIrisBotWebhookWorkerProfile struct {
	Version    int                                   `json:"version"`
	ProfileID  string                                `json:"profile_id"`
	Delivery   wireWebhookDeliveryWorkerProfile      `json:"delivery"`
	Receive    wireWebhookReceiveWorkerProfile       `json:"receive"`
	BotPool    wireBotPoolWorkerProfileField         `json:"bot_pool"`
	Validation irisBotWebhookWorkerProfileValidation `json:"validation"`
}

type wireBotPoolWorkerProfile struct {
	Workers   int `json:"workers"`
	QueueSize int `json:"queue_size"`
}

type wireBotPoolWorkerProfileField struct {
	value   wireBotPoolWorkerProfile
	present bool
}

func (f *wireBotPoolWorkerProfileField) UnmarshalJSON(data []byte) error {
	f.present = true
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(&f.value)
}

func (f wireBotPoolWorkerProfileField) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.value)
}

type wireWebhookDeliveryWorkerProfile struct {
	LaneWorkers             int                 `json:"lane_workers"`
	LaneQueueCapacity       int                 `json:"lane_queue_capacity"`
	MaxGlobalInFlight       int                 `json:"max_global_in_flight"`
	MaxPerEndpointInFlight  int                 `json:"max_per_endpoint_in_flight"`
	MaxDrainPerTick         int                 `json:"max_drain_per_tick"`
	MaxAttempts             int                 `json:"max_attempts"`
	RequestTimeoutMS        int                 `json:"request_timeout_ms"`
	LaneIdleTimeoutMS       int                 `json:"lane_idle_timeout_ms"`
	BreakerFailureThreshold optionalUint32Field `json:"breaker_failure_threshold"`
	BreakerCooldownMS       optionalIntField    `json:"breaker_cooldown_ms"`
}

type optionalUint32Field struct {
	value   uint32
	present bool
}

func (f *optionalUint32Field) UnmarshalJSON(data []byte) error {
	f.present = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("must not be null")
	}

	return json.Unmarshal(data, &f.value)
}

func (f optionalUint32Field) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.value)
}

type optionalIntField struct {
	value   int
	present bool
}

func (f *optionalIntField) UnmarshalJSON(data []byte) error {
	f.present = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("must not be null")
	}

	return json.Unmarshal(data, &f.value)
}

func (f optionalIntField) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.value)
}

type wireWebhookReceiveWorkerProfile struct {
	Workers          int   `json:"workers"`
	QueueSize        int   `json:"queue_size"`
	EnqueueTimeoutMS int   `json:"enqueue_timeout_ms"`
	HandlerTimeoutMS int   `json:"handler_timeout_ms"`
	MaxBodyBytes     int64 `json:"max_body_bytes"`
	DedupTTLMS       int   `json:"dedup_ttl_ms"`
	DedupTimeoutMS   int   `json:"dedup_timeout_ms"`
}

func defaultIrisBotWebhookWorkerProfile() IrisBotWebhookWorkerProfile {
	return IrisBotWebhookWorkerProfile{
		Version:   CurrentVersion,
		ProfileID: defaultProfileID,
		Delivery: irisWebhookDeliveryWorkerProfile{
			LaneWorkers:             defaultDeliveryLaneWorkers,
			LaneQueueCapacity:       defaultDeliveryLaneQueueCapacity,
			MaxGlobalInFlight:       defaultDeliveryMaxGlobalInFlight,
			MaxPerEndpointInFlight:  defaultDeliveryMaxPerEndpointInFlight,
			MaxDrainPerTick:         defaultDeliveryMaxDrainPerTick,
			MaxAttempts:             defaultDeliveryMaxAttempts,
			RequestTimeout:          defaultDeliveryRequestTimeout,
			LaneIdleTimeout:         defaultDeliveryLaneIdleTimeout,
			BreakerFailureThreshold: defaultDeliveryBreakerFailureThreshold,
			BreakerCooldown:         defaultDeliveryBreakerCooldown,
		},
		Receive: botWebhookReceiveWorkerProfile{
			Workers:        defaultReceiveWorkers,
			QueueSize:      defaultReceiveQueueSize,
			EnqueueTimeout: defaultReceiveEnqueueTimeout,
			HandlerTimeout: defaultReceiveHandlerTimeout,
			MaxBodyBytes:   defaultReceiveMaxBodyBytes,
			DedupTTL:       defaultReceiveDedupTTL,
			DedupTimeout:   defaultReceiveDedupTimeout,
		},
		BotPool: botPoolWorkerProfile{
			Workers:   defaultBotPoolWorkers,
			QueueSize: defaultBotPoolQueueSize,
		},
		Validation: irisBotWebhookWorkerProfileValidation{
			MinQueuePerEndpointMultiplier:          defaultMinQueuePerEndpointMultiplier,
			RequireReceiveCapacityForEndpointBurst: true,
		},
	}
}

func DefaultIrisBotWebhookWorkerProfile() IrisBotWebhookWorkerProfile {
	return defaultIrisBotWebhookWorkerProfile()
}

func (p IrisBotWebhookWorkerProfile) CanonicalJSON() []byte {
	data, err := json.Marshal(p.toWire())
	if err != nil {
		panic(fmt.Sprintf("marshal Iris bot webhook worker profile: %v", err))
	}
	return data
}

func (p IrisBotWebhookWorkerProfile) ProfileHash() string {
	sum := sha256.Sum256(p.CanonicalJSON())
	return hex.EncodeToString(sum[:])
}

func (p IrisBotWebhookWorkerProfile) Validate() error {
	var problems []string
	if p.Version != CurrentVersion {
		problems = append(problems, fmt.Sprintf("version must be %d", CurrentVersion))
	}
	if strings.TrimSpace(p.ProfileID) == "" {
		problems = append(problems, "profile_id must not be blank")
	}

	problems = appendBoundedInt(problems, p.Delivery.LaneWorkers, "delivery.lane_workers", maxWorkers)
	problems = appendBoundedInt(problems, p.Delivery.LaneQueueCapacity, "delivery.lane_queue_capacity", maxQueueCapacity)
	problems = appendBoundedInt(problems, p.Delivery.MaxGlobalInFlight, "delivery.max_global_in_flight", maxInFlight)
	problems = appendBoundedInt(problems, p.Delivery.MaxPerEndpointInFlight, "delivery.max_per_endpoint_in_flight", maxInFlight)
	problems = appendBoundedInt(problems, p.Delivery.MaxDrainPerTick, "delivery.max_drain_per_tick", maxDrainPerTick)
	problems = appendBoundedInt(problems, p.Delivery.MaxAttempts, "delivery.max_attempts", maxAttempts)
	problems = appendBoundedDuration(problems, p.Delivery.RequestTimeout, "delivery.request_timeout_ms", maxDuration)
	problems = appendBoundedDuration(problems, p.Delivery.LaneIdleTimeout, "delivery.lane_idle_timeout_ms", maxDuration)
	problems = appendBoundedInt(problems, p.Receive.Workers, "receive.workers", maxWorkers)
	problems = appendBoundedInt(problems, p.Receive.QueueSize, "receive.queue_size", maxQueueCapacity)
	problems = appendBoundedDuration(problems, p.Receive.EnqueueTimeout, "receive.enqueue_timeout_ms", maxDuration)
	problems = appendBoundedDuration(problems, p.Receive.HandlerTimeout, "receive.handler_timeout_ms", maxDuration)
	if p.Receive.MaxBodyBytes < 1 {
		problems = append(problems, "receive.max_body_bytes must be >= 1")
	} else if p.Receive.MaxBodyBytes > maxReceiveBodyBytes {
		problems = append(problems, fmt.Sprintf("receive.max_body_bytes must be <= %d", maxReceiveBodyBytes))
	}
	problems = appendBoundedDuration(problems, p.Receive.DedupTTL, "receive.dedup_ttl_ms", maxDuration)
	problems = appendBoundedDuration(problems, p.Receive.DedupTimeout, "receive.dedup_timeout_ms", maxDuration)
	problems = appendBoundedInt(problems, p.BotPool.Workers, "bot_pool.workers", maxWorkers)
	problems = appendBoundedInt(problems, p.BotPool.QueueSize, "bot_pool.queue_size", maxQueueCapacity)
	problems = appendBoundedInt(problems, p.Validation.MinQueuePerEndpointMultiplier, "validation.min_queue_per_endpoint_multiplier", maxQueueMultiplier)

	if len(problems) == 0 {
		problems = appendWorkerProfileRelationalProblems(problems, p)
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func appendWorkerProfileRelationalProblems(problems []string, profile IrisBotWebhookWorkerProfile) []string {
	if profile.Delivery.RequestTimeout > profile.Receive.HandlerTimeout+deliveryRequestTimeoutOverhang {
		problems = append(problems, "delivery.request_timeout_ms must be <= receive.handler_timeout_ms + 5000")
	}
	if profile.Delivery.MaxPerEndpointInFlight > profile.Delivery.MaxGlobalInFlight {
		problems = append(problems, "delivery.max_per_endpoint_in_flight must be <= delivery.max_global_in_flight")
	}
	if profile.Delivery.MaxGlobalInFlight > profile.Delivery.LaneQueueCapacity {
		problems = append(problems, "delivery.max_global_in_flight must be <= delivery.lane_queue_capacity")
	}
	if profile.Validation.RequireReceiveCapacityForEndpointBurst {
		minQueue := profile.Delivery.MaxPerEndpointInFlight * profile.Validation.MinQueuePerEndpointMultiplier
		if profile.Receive.QueueSize < minQueue {
			problems = append(problems, "receive.queue_size must be >= delivery.max_per_endpoint_in_flight * validation.min_queue_per_endpoint_multiplier")
		}
		if profile.Receive.Workers < ceilDiv(profile.Delivery.MaxPerEndpointInFlight, 2) {
			problems = append(problems, "receive.workers must be >= ceil(delivery.max_per_endpoint_in_flight / 2)")
		}
	}
	if profile.Delivery.MaxGlobalInFlight > profile.Receive.Workers+profile.Receive.QueueSize {
		problems = append(problems, "delivery.max_global_in_flight must be <= receive.workers + receive.queue_size")
	}

	breakerProblemStart := len(problems)
	problems = appendBreakerCooldownProblems(problems, profile.Delivery)
	if len(problems) == breakerProblemStart {
		problems = appendDeliveryHorizonProblem(problems, profile.Delivery)
	}
	if len(problems) == 0 {
		problems = appendDedupHorizonProblem(problems, profile)
	}

	return problems
}

func appendBreakerCooldownProblems(problems []string, delivery irisWebhookDeliveryWorkerProfile) []string {
	switch {
	case delivery.BreakerCooldown < 0:
		return append(problems, "delivery.breaker_cooldown_ms must be >= 0")
	case delivery.BreakerCooldown > maxDuration:
		return append(problems, fmt.Sprintf("delivery.breaker_cooldown_ms must be <= %s", maxDuration))
	case delivery.BreakerCooldown%time.Millisecond != 0:
		return append(problems, "delivery.breaker_cooldown_ms must use whole milliseconds")
	case delivery.BreakerFailureThreshold > 0 && delivery.BreakerCooldown == 0:
		return append(problems, "delivery.breaker_cooldown_ms must be > 0 when breaker_failure_threshold > 0")
	default:
		return problems
	}
}

func appendDeliveryHorizonProblem(problems []string, delivery irisWebhookDeliveryWorkerProfile) []string {
	horizon := maxDeliveryHorizon(delivery)
	if horizon < maxDuration {
		return problems
	}
	if delivery.BreakerFailureThreshold > 0 && delivery.BreakerCooldown > deliveryRetryWaitCeiling {
		return append(problems, fmt.Sprintf(
			"delivery.max_attempts=%d, delivery.request_timeout_ms=%d, and delivery.breaker_cooldown_ms=%d produce a maximum delivery horizon of %s; reduce their combination so the horizon is < %s",
			delivery.MaxAttempts,
			delivery.RequestTimeout/time.Millisecond,
			delivery.BreakerCooldown/time.Millisecond,
			horizon,
			maxDuration,
		))
	}
	return append(problems, fmt.Sprintf(
		"delivery.max_attempts=%d and delivery.request_timeout_ms=%d produce a maximum delivery horizon of %s; reduce their combination so the horizon is < %s",
		delivery.MaxAttempts,
		delivery.RequestTimeout/time.Millisecond,
		horizon,
		maxDuration,
	))
}

func appendDedupHorizonProblem(problems []string, profile IrisBotWebhookWorkerProfile) []string {
	horizon := maxDeliveryHorizon(profile.Delivery)
	if profile.Receive.DedupTTL <= horizon {
		return append(problems, fmt.Sprintf(
			"receive.dedup_ttl_ms must be greater than the maximum delivery horizon %s (minimum %s)",
			horizon,
			horizon+time.Millisecond,
		))
	}
	return problems
}

func maxDeliveryHorizon(delivery irisWebhookDeliveryWorkerProfile) time.Duration {
	waitCeiling := deliveryRetryWaitCeiling
	if delivery.BreakerFailureThreshold > 0 && delivery.BreakerCooldown > waitCeiling {
		waitCeiling = delivery.BreakerCooldown
	}
	attempts := int64(delivery.MaxAttempts)
	nanoseconds := attempts*int64(delivery.RequestTimeout) + (attempts-1)*int64(waitCeiling)
	return time.Duration(nanoseconds)
}

func (p IrisBotWebhookWorkerProfile) toWire() wireIrisBotWebhookWorkerProfile {
	return wireIrisBotWebhookWorkerProfile{
		Version:   p.Version,
		ProfileID: strings.TrimSpace(p.ProfileID),
		Delivery: wireWebhookDeliveryWorkerProfile{
			LaneWorkers:             p.Delivery.LaneWorkers,
			LaneQueueCapacity:       p.Delivery.LaneQueueCapacity,
			MaxGlobalInFlight:       p.Delivery.MaxGlobalInFlight,
			MaxPerEndpointInFlight:  p.Delivery.MaxPerEndpointInFlight,
			MaxDrainPerTick:         p.Delivery.MaxDrainPerTick,
			MaxAttempts:             p.Delivery.MaxAttempts,
			RequestTimeoutMS:        int(p.Delivery.RequestTimeout / time.Millisecond),
			LaneIdleTimeoutMS:       int(p.Delivery.LaneIdleTimeout / time.Millisecond),
			BreakerFailureThreshold: optionalUint32Field{value: p.Delivery.BreakerFailureThreshold, present: true},
			BreakerCooldownMS:       optionalIntField{value: int(p.Delivery.BreakerCooldown / time.Millisecond), present: true},
		},
		Receive: wireWebhookReceiveWorkerProfile{
			Workers:          p.Receive.Workers,
			QueueSize:        p.Receive.QueueSize,
			EnqueueTimeoutMS: int(p.Receive.EnqueueTimeout / time.Millisecond),
			HandlerTimeoutMS: int(p.Receive.HandlerTimeout / time.Millisecond),
			MaxBodyBytes:     p.Receive.MaxBodyBytes,
			DedupTTLMS:       int(p.Receive.DedupTTL / time.Millisecond),
			DedupTimeoutMS:   int(p.Receive.DedupTimeout / time.Millisecond),
		},
		BotPool: wireBotPoolWorkerProfileField{
			value: wireBotPoolWorkerProfile{
				Workers:   p.BotPool.Workers,
				QueueSize: p.BotPool.QueueSize,
			},
			present: true,
		},
		Validation: p.Validation,
	}
}

func fromWire(wire wireIrisBotWebhookWorkerProfile) IrisBotWebhookWorkerProfile {
	breakerFailureThreshold := defaultDeliveryBreakerFailureThreshold
	if wire.Delivery.BreakerFailureThreshold.present {
		breakerFailureThreshold = wire.Delivery.BreakerFailureThreshold.value
	}
	breakerCooldown := defaultDeliveryBreakerCooldown
	if wire.Delivery.BreakerCooldownMS.present {
		breakerCooldown = boundedDurationFromMilliseconds(wire.Delivery.BreakerCooldownMS.value)
	}
	botPool := botPoolWorkerProfile{
		Workers:   defaultBotPoolWorkers,
		QueueSize: defaultBotPoolQueueSize,
	}
	if wire.BotPool.present {
		botPool = botPoolWorkerProfile{
			Workers:   wire.BotPool.value.Workers,
			QueueSize: wire.BotPool.value.QueueSize,
		}
	}

	return IrisBotWebhookWorkerProfile{
		Version:   wire.Version,
		ProfileID: strings.TrimSpace(wire.ProfileID),
		Delivery: irisWebhookDeliveryWorkerProfile{
			LaneWorkers:             wire.Delivery.LaneWorkers,
			LaneQueueCapacity:       wire.Delivery.LaneQueueCapacity,
			MaxGlobalInFlight:       wire.Delivery.MaxGlobalInFlight,
			MaxPerEndpointInFlight:  wire.Delivery.MaxPerEndpointInFlight,
			MaxDrainPerTick:         wire.Delivery.MaxDrainPerTick,
			MaxAttempts:             wire.Delivery.MaxAttempts,
			RequestTimeout:          boundedDurationFromMilliseconds(wire.Delivery.RequestTimeoutMS),
			LaneIdleTimeout:         boundedDurationFromMilliseconds(wire.Delivery.LaneIdleTimeoutMS),
			BreakerFailureThreshold: breakerFailureThreshold,
			BreakerCooldown:         breakerCooldown,
		},
		Receive: botWebhookReceiveWorkerProfile{
			Workers:        wire.Receive.Workers,
			QueueSize:      wire.Receive.QueueSize,
			EnqueueTimeout: boundedDurationFromMilliseconds(wire.Receive.EnqueueTimeoutMS),
			HandlerTimeout: boundedDurationFromMilliseconds(wire.Receive.HandlerTimeoutMS),
			MaxBodyBytes:   wire.Receive.MaxBodyBytes,
			DedupTTL:       boundedDurationFromMilliseconds(wire.Receive.DedupTTLMS),
			DedupTimeout:   boundedDurationFromMilliseconds(wire.Receive.DedupTimeoutMS),
		},
		BotPool:    botPool,
		Validation: wire.Validation,
	}
}

// boundedDurationFromMilliseconds는 raw integer가 time.Duration 곱셈에서 wrap되기 전에
// 기존 Validate가 같은 field의 범위 오류로 집계할 수 있는 sentinel로 정규화한다.
func boundedDurationFromMilliseconds(value int) time.Duration {
	if value < 0 {
		return -time.Millisecond
	}
	if value > int(maxDuration/time.Millisecond) {
		return maxDuration + time.Millisecond
	}

	return time.Duration(value) * time.Millisecond
}

func appendBoundedInt(problems []string, value int, name string, maxValue int) []string {
	if value < 1 {
		return append(problems, name+" must be >= 1")
	}
	if value > maxValue {
		return append(problems, fmt.Sprintf("%s must be <= %d", name, maxValue))
	}
	return problems
}

func appendBoundedDuration(problems []string, value time.Duration, name string, maxValue time.Duration) []string {
	if value <= 0 {
		return append(problems, name+" must be > 0")
	}
	if value > maxValue {
		return append(problems, fmt.Sprintf("%s must be <= %s", name, maxValue))
	}
	if value%time.Millisecond != 0 {
		return append(problems, name+" must use whole milliseconds")
	}
	return problems
}

func ceilDiv(value, divisor int) int {
	return (value + divisor - 1) / divisor
}
