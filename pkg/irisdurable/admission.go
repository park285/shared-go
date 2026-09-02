package irisdurable

import (
	"context"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

// AdmissionInput은 durable inbox에 commit할 webhook 메시지의 식별 정보다.
type AdmissionInput struct {
	// MessageID는 봇이 정규화한 dedup 키다. 같은 값은 두 번 admit되지 않는다.
	MessageID string
	// OrderingKey는 처리 순서를 직렬화하는 단위(보통 방)다.
	OrderingKey string
	// Payload는 저장할 메시지 본문(JSON)이다.
	Payload []byte
}

// Admitter는 HTTP 200 전에 메시지를 durable store에 commit하는 저장소 계약이다.
//
//   - 처음 보는 MessageID는 AdmissionAccepted와 nil을 반환한다.
//   - 이미 commit된 MessageID는 AdmissionDuplicate와 nil을 반환한다.
//   - commit 여부를 알 수 없으면 AdmissionOutcomeUnknown과 오류를 반환한다.
//   - 그 외 실패는 AdmissionFailed 또는 AdmissionRejected와 오류를 반환한다.
//
// 오류를 동반한 결과는 호출자가 503으로 매핑해 Iris 재전송을 유도하고, 재전송은 Duplicate로 흡수된다.
type Admitter interface {
	Admit(ctx context.Context, input AdmissionInput) (workercontract.AdmissionResult, error)
}
