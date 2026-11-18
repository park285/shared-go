package irisdurable

import (
	"context"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

type AdmissionInput struct {
	MessageID   string
	OrderingKey string
	Payload     []byte
}

type Admitter interface {
	Admit(ctx context.Context, input AdmissionInput) (workercontract.AdmissionResult, error)
}
