package health

import "testing"

func TestGetReadinessAggregatesComponentState(t *testing.T) {
	RemoveComponent("youtube")
	t.Cleanup(func() { RemoveComponent("youtube") })

	SetComponent("youtube", ComponentStatus{Ready: true, Degraded: true})

	response, ready := GetReadiness()
	if !ready || response.Status != "ok" {
		t.Fatalf("GetReadiness() = (%#v, %v), want ready degraded response", response, ready)
	}

	if got := response.Components["youtube"]; !got.Ready || !got.Degraded {
		t.Fatalf("youtube component = %#v", got)
	}

	SetComponent("youtube", ComponentStatus{Ready: false, Degraded: true})

	response, ready = GetReadiness()
	if ready || response.Status != "not_ready" {
		t.Fatalf("GetReadiness() = (%#v, %v), want not ready", response, ready)
	}
}
