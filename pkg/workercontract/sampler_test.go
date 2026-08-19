package workercontract_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/workercontract"
)

func TestQueueSamplerFailureOmitsCurrentValuesAndKeepsLastSuccessTime(t *testing.T) {
	fail := false
	sampler := workercontract.NewQueueSampler(func(context.Context) (workercontract.QueueValues, error) {
		if fail {
			return workercontract.QueueValues{}, errors.New("query failed")
		}
		return workercontract.QueueValues{Depth: 3, OldestQueuedAge: 2 * time.Second}, nil
	})
	first := sampler.Sample(context.Background(), time.Unix(100, 0))
	if first.Status != workercontract.QueueSnapshotCurrent || first.Depth == nil || *first.Depth != 3 {
		t.Fatalf("first snapshot = %+v", first)
	}
	fail = true
	second := sampler.Sample(context.Background(), time.Unix(101, 0))
	if second.Status != workercontract.QueueSnapshotUnavailable || second.Depth != nil || second.OldestQueuedAgeMS != nil {
		t.Fatalf("failed snapshot = %+v", second)
	}
	if second.LastSuccessAtEpochMS == nil || *second.LastSuccessAtEpochMS != time.Unix(100, 0).UnixMilli() {
		t.Fatalf("last success = %v", second.LastSuccessAtEpochMS)
	}
}

func TestQueueSamplerBoundsSourceWithQueryTimeout(t *testing.T) {
	sampler := workercontract.NewQueueSampler(func(ctx context.Context) (workercontract.QueueValues, error) {
		<-ctx.Done()
		return workercontract.QueueValues{}, ctx.Err()
	})
	started := time.Now()
	snapshot := sampler.Sample(context.Background(), started)
	if elapsed := time.Since(started); elapsed < workercontract.QueueSampleTimeout || elapsed > workercontract.QueueSampleTimeout+time.Second {
		t.Fatalf("Sample() elapsed = %v", elapsed)
	}
	if snapshot.ErrorCode == nil || *snapshot.ErrorCode != workercontract.QueueSnapshotFailed {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}
