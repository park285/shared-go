// Package logging은 slog 구조화 로깅·민감값 마스킹·파일 보관·추적 상관관계를 제공한다.
// 호출자의 이벤트·메시지·속성에 컨텍스트의 request_id·job_id·runtime·component를 병합한다.
// job_id는 RunOperation이 컨텍스트에 없을 때 새로 만들어 채운다.
// 기록 헬퍼(Debug·Info·Warn·Error·Log·LogAndWrapError·RunOperation)는 logger가 nil이면
// 기록 시점의 slog.Default()에 기록한다.
//
// credential 키 규칙은 이 패키지가 소유하고, 개인 식별자 키(예: room, sender)는 서비스가
// Options.PrivacyKeys로 넘긴다. 내장 privacy 키는 없으므로 room_id·user_id처럼 넘기지 않은 키는
// 보존된다. 마스킹은 slog 속성·group과 중첩 map[string]any에서 깊이 8까지 적용하며
// map[string]string·[]any 내부 맵·구조체는 제외한다. 보존·마스킹 키 표는 README가 소유한다.
//
// # 사용 예
//
//	ctx = logging.WithComponent(ctx, "poller")
//	err := logging.RunOperation(ctx, logger, logging.OperationOptions{
//	    Name: "ingest.batch",
//	}, func(ctx context.Context) error {
//	    return process(ctx)
//	})
//
//	logging.Info(ctx, logger, "youtube.poll.started", "poll started",
//	    logging.Runtime("youtube-producer"),
//	)
package logging
