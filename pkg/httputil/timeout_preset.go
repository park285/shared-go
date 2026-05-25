package httputil

import (
	"net/http"
	"time"
)

type TimeoutPreset int

const (
	FetchTimeout TimeoutPreset = iota + 1
	LongPollTimeout
	ScraperTimeout
)

func (p TimeoutPreset) Duration() time.Duration {
	switch p {
	case FetchTimeout:
		return 10 * time.Second
	case LongPollTimeout:
		return 30 * time.Second
	case ScraperTimeout:
		return 15 * time.Second
	default:
		return 30 * time.Second
	}
}

func NewClientWithPreset(preset TimeoutPreset) *http.Client {
	return NewClient(preset.Duration())
}

func NewExternalAPIClientWithPreset(preset TimeoutPreset) *http.Client {
	return NewExternalAPIClient(preset.Duration())
}

func NewInternalServiceClientWithPreset(preset TimeoutPreset) *http.Client {
	return NewInternalServiceClient(preset.Duration())
}
