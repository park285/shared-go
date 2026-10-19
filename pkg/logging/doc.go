// Package logging은 slog 구조화 로깅·민감값 마스킹·파일 보관·추적 상관관계를 제공한다.
// 호출자의 이벤트·메시지·속성에 컨텍스트의 request_id·job_id·runtime·component를 병합한다.
//
// room_id·user_id는 상관관계 식별자로 보존한다. 나머지 개인정보 키는 slog 속성과
// 중첩 map[string]any에서 깊이 8까지 마스킹하며 map[string]string·[]any 내부 맵·구조체는 제외한다.
//
// # 사용 예
//
//	ctx = logging.WithJobID(ctx, "ingest-job")
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
