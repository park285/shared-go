package health

import (
	"maps"
	"runtime"
	"sync"
	"time"
)

var (
	startTime  time.Time
	version    = "dev"
	initOnce   sync.Once
	components = make(map[string]ComponentStatus)
	componentM sync.RWMutex
)

func Init(v string) {
	initOnce.Do(func() {
		startTime = time.Now()

		if v != "" {
			version = v
		}
	})
}

type Response struct {
	Status     string                     `json:"status"`
	Version    string                     `json:"version"`
	Uptime     string                     `json:"uptime"`
	Goroutines int                        `json:"goroutines"`
	Components map[string]ComponentStatus `json:"components,omitempty"`
}

type ComponentStatus struct {
	Ready    bool `json:"ready"`
	Degraded bool `json:"degraded"`
}

func Get() Response {
	return Response{
		Status:     "ok",
		Version:    version,
		Uptime:     formatDuration(time.Since(startTime)),
		Goroutines: runtime.NumGoroutine(),
		Components: componentSnapshot(),
	}
}

func GetReadiness() (Response, bool) {
	response := Get()
	ready := true

	for _, component := range response.Components {
		if !component.Ready {
			ready = false
			break
		}
	}

	if !ready {
		response.Status = "not_ready"
	}

	return response, ready
}

func SetComponent(name string, status ComponentStatus) {
	componentM.Lock()

	components[name] = status
	componentM.Unlock()
}

func RemoveComponent(name string) {
	componentM.Lock()
	delete(components, name)
	componentM.Unlock()
}

func componentSnapshot() map[string]ComponentStatus {
	componentM.RLock()
	defer componentM.RUnlock()

	if len(components) == 0 {
		return nil
	}

	snapshot := make(map[string]ComponentStatus, len(components))
	maps.Copy(snapshot, components)

	return snapshot
}

func GetVersion() string {
	return version
}

func GetUptime() string {
	return formatDuration(time.Since(startTime))
}

func formatDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}
