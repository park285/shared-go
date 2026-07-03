package workerconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	CurrentVersion = 1
)

const (
	defaultProfileID = "default"

	defaultDeliveryLaneWorkers            = 32
	defaultDeliveryLaneQueueCapacity      = defaultDeliveryLaneWorkers * 4
	defaultDeliveryMaxGlobalInFlight      = 32
	defaultDeliveryMaxPerEndpointInFlight = 8
	defaultDeliveryMaxDrainPerTick        = 128
	defaultDeliveryMaxAttempts            = 6
	defaultDeliveryRequestTimeout         = 125 * time.Second
	defaultDeliveryLaneIdleTimeout        = 750 * time.Millisecond

	defaultReceiveWorkers        = 16
	defaultReceiveQueueSize      = 1000
	defaultReceiveEnqueueTimeout = 50 * time.Millisecond
	defaultReceiveHandlerTimeout = 120 * time.Second
	defaultReceiveMaxBodyBytes   = 64 << 10
	defaultReceiveDedupTTL       = 60 * time.Second
	defaultReceiveDedupTimeout   = 200 * time.Millisecond

	defaultBotPoolWorkers   = 10
	defaultBotPoolQueueSize = 100

	defaultMinQueuePerEndpointMultiplier = 4
	timeoutNetworkMargin                 = 5 * time.Second

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
	Delivery   IrisWebhookDeliveryWorkerProfile      `json:"delivery"`
	Receive    BotWebhookReceiveWorkerProfile        `json:"receive"`
	BotPool    BotPoolWorkerProfile                  `json:"bot_pool"`
	Validation IrisBotWebhookWorkerProfileValidation `json:"validation"`
}

type IrisWebhookDeliveryWorkerProfile struct {
	LaneWorkers            int           `json:"-"`
	LaneQueueCapacity      int           `json:"-"`
	MaxGlobalInFlight      int           `json:"-"`
	MaxPerEndpointInFlight int           `json:"-"`
	MaxDrainPerTick        int           `json:"-"`
	MaxAttempts            int           `json:"-"`
	RequestTimeout         time.Duration `json:"-"`
	LaneIdleTimeout        time.Duration `json:"-"`
}

type BotWebhookReceiveWorkerProfile struct {
	Workers        int           `json:"-"`
	QueueSize      int           `json:"-"`
	EnqueueTimeout time.Duration `json:"-"`
	HandlerTimeout time.Duration `json:"-"`
	MaxBodyBytes   int64         `json:"-"`
	DedupTTL       time.Duration `json:"-"`
	DedupTimeout   time.Duration `json:"-"`
}

type BotPoolWorkerProfile struct {
	Workers   int `json:"-"`
	QueueSize int `json:"-"`
}

type IrisBotWebhookWorkerProfileValidation struct {
	MinQueuePerEndpointMultiplier          int  `json:"min_queue_per_endpoint_multiplier"`
	RequireReceiveCapacityForEndpointBurst bool `json:"require_receive_capacity_for_endpoint_burst"`
}

type wireIrisBotWebhookWorkerProfile struct {
	Version    int                                   `json:"version"`
	ProfileID  string                                `json:"profile_id"`
	Delivery   wireWebhookDeliveryWorkerProfile      `json:"delivery"`
	Receive    wireWebhookReceiveWorkerProfile       `json:"receive"`
	BotPool    wireBotPoolWorkerProfileField         `json:"bot_pool"`
	Validation IrisBotWebhookWorkerProfileValidation `json:"validation"`
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
	LaneWorkers            int `json:"lane_workers"`
	LaneQueueCapacity      int `json:"lane_queue_capacity"`
	MaxGlobalInFlight      int `json:"max_global_in_flight"`
	MaxPerEndpointInFlight int `json:"max_per_endpoint_in_flight"`
	MaxDrainPerTick        int `json:"max_drain_per_tick"`
	MaxAttempts            int `json:"max_attempts"`
	RequestTimeoutMS       int `json:"request_timeout_ms"`
	LaneIdleTimeoutMS      int `json:"lane_idle_timeout_ms"`
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

func DefaultIrisBotWebhookWorkerProfile() IrisBotWebhookWorkerProfile {
	return IrisBotWebhookWorkerProfile{
		Version:   CurrentVersion,
		ProfileID: defaultProfileID,
		Delivery: IrisWebhookDeliveryWorkerProfile{
			LaneWorkers:            defaultDeliveryLaneWorkers,
			LaneQueueCapacity:      defaultDeliveryLaneQueueCapacity,
			MaxGlobalInFlight:      defaultDeliveryMaxGlobalInFlight,
			MaxPerEndpointInFlight: defaultDeliveryMaxPerEndpointInFlight,
			MaxDrainPerTick:        defaultDeliveryMaxDrainPerTick,
			MaxAttempts:            defaultDeliveryMaxAttempts,
			RequestTimeout:         defaultDeliveryRequestTimeout,
			LaneIdleTimeout:        defaultDeliveryLaneIdleTimeout,
		},
		Receive: BotWebhookReceiveWorkerProfile{
			Workers:        defaultReceiveWorkers,
			QueueSize:      defaultReceiveQueueSize,
			EnqueueTimeout: defaultReceiveEnqueueTimeout,
			HandlerTimeout: defaultReceiveHandlerTimeout,
			MaxBodyBytes:   defaultReceiveMaxBodyBytes,
			DedupTTL:       defaultReceiveDedupTTL,
			DedupTimeout:   defaultReceiveDedupTimeout,
		},
		BotPool: BotPoolWorkerProfile{
			Workers:   defaultBotPoolWorkers,
			QueueSize: defaultBotPoolQueueSize,
		},
		Validation: IrisBotWebhookWorkerProfileValidation{
			MinQueuePerEndpointMultiplier:          defaultMinQueuePerEndpointMultiplier,
			RequireReceiveCapacityForEndpointBurst: true,
		},
	}
}

func LegacyIrisBotWebhookWorkerProfile() IrisBotWebhookWorkerProfile {
	profile := DefaultIrisBotWebhookWorkerProfile()
	profile.ProfileID = "legacy-hardcoded"
	return profile
}

func DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics(reader io.Reader) (IrisBotWebhookWorkerProfile, error) {
	var diagnostics struct {
		Workers struct {
			Webhook struct {
				WebhookPipeline struct {
					ProfileEnabled *bool                            `json:"profileEnabled"`
					WorkerProfile  *wireIrisBotWebhookWorkerProfile `json:"workerProfile"`
				} `json:"webhookPipeline"`
			} `json:"webhook"`
		} `json:"workers"`
	}
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&diagnostics); err != nil {
		return IrisBotWebhookWorkerProfile{}, err
	}
	if diagnostics.Workers.Webhook.WebhookPipeline.ProfileEnabled == nil {
		return IrisBotWebhookWorkerProfile{}, errors.New("diagnostics workers.webhook.webhookPipeline.profileEnabled is missing")
	}
	if !*diagnostics.Workers.Webhook.WebhookPipeline.ProfileEnabled {
		return LegacyIrisBotWebhookWorkerProfile(), ErrWorkerProfileDisabled
	}
	if diagnostics.Workers.Webhook.WebhookPipeline.WorkerProfile == nil {
		return IrisBotWebhookWorkerProfile{}, errors.New("diagnostics workers.webhook.webhookPipeline.workerProfile is missing")
	}
	profile := fromWire(*diagnostics.Workers.Webhook.WebhookPipeline.WorkerProfile)
	if err := profile.Validate(); err != nil {
		return IrisBotWebhookWorkerProfile{}, err
	}
	return profile, nil
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
		if p.Delivery.MaxPerEndpointInFlight > p.Delivery.MaxGlobalInFlight {
			problems = append(problems, "delivery.max_per_endpoint_in_flight must be <= delivery.max_global_in_flight")
		}
		if p.Delivery.MaxGlobalInFlight > p.Delivery.LaneQueueCapacity {
			problems = append(problems, "delivery.max_global_in_flight must be <= delivery.lane_queue_capacity")
		}
		if p.Validation.RequireReceiveCapacityForEndpointBurst {
			minQueue := p.Delivery.MaxPerEndpointInFlight * p.Validation.MinQueuePerEndpointMultiplier
			if p.Receive.QueueSize < minQueue {
				problems = append(problems, "receive.queue_size must be >= delivery.max_per_endpoint_in_flight * validation.min_queue_per_endpoint_multiplier")
			}
			if p.Receive.Workers < ceilDiv(p.Delivery.MaxPerEndpointInFlight, 2) {
				problems = append(problems, "receive.workers must be >= ceil(delivery.max_per_endpoint_in_flight / 2)")
			}
		}
		if p.Delivery.MaxGlobalInFlight > p.Receive.Workers+p.Receive.QueueSize {
			problems = append(problems, "delivery.max_global_in_flight must be <= receive.workers + receive.queue_size")
		}
		if p.Delivery.RequestTimeout > p.Receive.HandlerTimeout+timeoutNetworkMargin {
			problems = append(problems, "delivery.request_timeout_ms must fit receive.handler_timeout_ms plus network margin")
		}
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (p IrisBotWebhookWorkerProfile) toWire() wireIrisBotWebhookWorkerProfile {
	return wireIrisBotWebhookWorkerProfile{
		Version:   p.Version,
		ProfileID: strings.TrimSpace(p.ProfileID),
		Delivery: wireWebhookDeliveryWorkerProfile{
			LaneWorkers:            p.Delivery.LaneWorkers,
			LaneQueueCapacity:      p.Delivery.LaneQueueCapacity,
			MaxGlobalInFlight:      p.Delivery.MaxGlobalInFlight,
			MaxPerEndpointInFlight: p.Delivery.MaxPerEndpointInFlight,
			MaxDrainPerTick:        p.Delivery.MaxDrainPerTick,
			MaxAttempts:            p.Delivery.MaxAttempts,
			RequestTimeoutMS:       int(p.Delivery.RequestTimeout / time.Millisecond),
			LaneIdleTimeoutMS:      int(p.Delivery.LaneIdleTimeout / time.Millisecond),
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
	botPool := BotPoolWorkerProfile{
		Workers:   defaultBotPoolWorkers,
		QueueSize: defaultBotPoolQueueSize,
	}
	if wire.BotPool.present {
		botPool = BotPoolWorkerProfile{
			Workers:   wire.BotPool.value.Workers,
			QueueSize: wire.BotPool.value.QueueSize,
		}
	}

	return IrisBotWebhookWorkerProfile{
		Version:   wire.Version,
		ProfileID: strings.TrimSpace(wire.ProfileID),
		Delivery: IrisWebhookDeliveryWorkerProfile{
			LaneWorkers:            wire.Delivery.LaneWorkers,
			LaneQueueCapacity:      wire.Delivery.LaneQueueCapacity,
			MaxGlobalInFlight:      wire.Delivery.MaxGlobalInFlight,
			MaxPerEndpointInFlight: wire.Delivery.MaxPerEndpointInFlight,
			MaxDrainPerTick:        wire.Delivery.MaxDrainPerTick,
			MaxAttempts:            wire.Delivery.MaxAttempts,
			RequestTimeout:         time.Duration(wire.Delivery.RequestTimeoutMS) * time.Millisecond,
			LaneIdleTimeout:        time.Duration(wire.Delivery.LaneIdleTimeoutMS) * time.Millisecond,
		},
		Receive: BotWebhookReceiveWorkerProfile{
			Workers:        wire.Receive.Workers,
			QueueSize:      wire.Receive.QueueSize,
			EnqueueTimeout: time.Duration(wire.Receive.EnqueueTimeoutMS) * time.Millisecond,
			HandlerTimeout: time.Duration(wire.Receive.HandlerTimeoutMS) * time.Millisecond,
			MaxBodyBytes:   wire.Receive.MaxBodyBytes,
			DedupTTL:       time.Duration(wire.Receive.DedupTTLMS) * time.Millisecond,
			DedupTimeout:   time.Duration(wire.Receive.DedupTimeoutMS) * time.Millisecond,
		},
		BotPool:    botPool,
		Validation: wire.Validation,
	}
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
	return problems
}

func ceilDiv(value, divisor int) int {
	return (value + divisor - 1) / divisor
}
