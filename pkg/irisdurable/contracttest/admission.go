package contracttest

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func runAdmission(t *testing.T, newAdmitter func(*testing.T) irisdurable.Admitter) {
	t.Helper()

	t.Run("FirstAcceptedThenDuplicate", func(t *testing.T) { testAdmissionIdempotent(t, newAdmitter(t)) })
	t.Run("DistinctMessagesAreIndependent", func(t *testing.T) { testAdmissionIndependent(t, newAdmitter(t)) })
	t.Run("ConcurrentSameMessageAdmitsOnce", func(t *testing.T) { testAdmissionConcurrent(t, newAdmitter(t)) })
	t.Run("CanceledContextFailsClosed", func(t *testing.T) { testAdmissionCanceled(t, newAdmitter(t)) })
}

func testAdmissionIdempotent(t *testing.T, admitter irisdurable.Admitter) {
	t.Helper()

	input := newAdmissionInput()

	requireAdmission(t, admitter, input, workercontract.AdmissionAccepted)
	requireAdmission(t, admitter, input, workercontract.AdmissionDuplicate)
}

func testAdmissionIndependent(t *testing.T, admitter irisdurable.Admitter) {
	t.Helper()

	first := newAdmissionInput()
	second := newAdmissionInput()

	second.OrderingKey = first.OrderingKey

	requireAdmission(t, admitter, first, workercontract.AdmissionAccepted)
	requireAdmission(t, admitter, second, workercontract.AdmissionAccepted)
	requireAdmission(t, admitter, first, workercontract.AdmissionDuplicate)
}

func testAdmissionConcurrent(t *testing.T, admitter irisdurable.Admitter) {
	t.Helper()

	const attempts = 8

	input := newAdmissionInput()
	results := make([]workercontract.AdmissionResult, attempts)
	errs := make([]error, attempts)

	var wg sync.WaitGroup

	for i := range attempts {
		wg.Go(func() { results[i], errs[i] = admitter.Admit(t.Context(), input) })
	}

	wg.Wait()

	accepted := 0

	for i := range attempts {
		if errs[i] != nil {
			t.Fatalf("concurrent attempt %d: %v", i, errs[i])
		}

		switch results[i] {
		case workercontract.AdmissionAccepted:
			accepted++
		case workercontract.AdmissionDuplicate:
		case workercontract.AdmissionRejected, workercontract.AdmissionFailed, workercontract.AdmissionOutcomeUnknown:
			t.Fatalf("concurrent attempt %d result = %s; want accepted or duplicate", i, results[i])
		}
	}

	if accepted != 1 {
		t.Fatalf("accepted %d times; want exactly 1", accepted)
	}
}

func testAdmissionCanceled(t *testing.T, admitter irisdurable.Admitter) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result, err := admitter.Admit(ctx, newAdmissionInput())
	if err == nil {
		t.Fatal("admit with canceled context must return an error")
	}

	if result == workercontract.AdmissionAccepted || result == workercontract.AdmissionDuplicate {
		t.Fatalf("result = %s alongside an error; accepted and duplicate never carry an error", result)
	}
}

func newAdmissionInput() irisdurable.AdmissionInput {
	id := uniqueID("msg")

	return irisdurable.AdmissionInput{
		MessageID:   id,
		OrderingKey: uniqueID("room"),
		Payload:     fmt.Appendf(nil, `{"messageId":%q,"contract":"irisdurable"}`, id),
	}
}

func requireAdmission(t *testing.T, admitter irisdurable.Admitter, input irisdurable.AdmissionInput, want workercontract.AdmissionResult) {
	t.Helper()

	result, err := admitter.Admit(t.Context(), input)
	if err != nil {
		t.Fatalf("admit %s: %v", input.MessageID, err)
	}

	if result != want {
		t.Fatalf("admit %s = %s; want %s", input.MessageID, result, want)
	}
}
