package irisdurable

import "time"

const (
	// IrisReplyAdmissionRetention은 Iris reply admission 원장의 보존 기간이다. Iris의
	// DEFAULT_REPLY_ADMISSION_RETENTION과 같아야 하며 check-stack-retry-contract.sh가 대조한다.
	IrisReplyAdmissionRetention = 168 * time.Hour

	// ReplayHorizonMargin은 자동 replay 지평과 Iris admission 보존 사이의 여유다. 지평 끝에서
	// 재발송한 요청도 Iris가 같은 clientRequestId를 아직 기억해야 중복 발화가 없다.
	ReplayHorizonMargin = 24 * time.Hour

	// AutomaticReplayHorizon은 저장된 clientRequestId로 자동 재발송을 허용하는 상한이다.
	AutomaticReplayHorizon = IrisReplyAdmissionRetention - ReplayHorizonMargin

	// ReplyOutboxMinRetention은 reply outbox 행이 Iris admission 보존을 덮도록 하는 최소 보존이다.
	ReplyOutboxMinRetention = IrisReplyAdmissionRetention

	// RetryBackoffMaxShift는 inbox 재시도 지수 백오프에서 지수(2^n)의 상한이다.
	RetryBackoffMaxShift = 8
)
