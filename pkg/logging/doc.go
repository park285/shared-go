// Package logging은 runtime 서비스용 공용 구조화 로깅 helper를 제공합니다.
//
// # 패키지 개요
//
// 이 패키지는 slog 기반 구조화 로깅을 공통화합니다. 호출부는 event, message,
// slog.Attr를 전달하고, 패키지는 context에 전파된 request_id, job_id, runtime,
// component 값을 자동으로 병합합니다.
//
// operation lifecycle 로깅, 민감값 마스킹 handler, 파일 로깅과 압축 archive,
// OpenTelemetry trace/span 상관관계 주입도 이 패키지의 책임입니다. room_id와 user_id는
// 운영 상관관계 ID로 보존하고, 나머지 privacy exact key는 slog attr과 중첩 map[string]any에서 depth 8까지 재귀적으로 마스킹하며,
// map[string]string, []any 내부 map, struct 필드는 대상이 아닙니다. 서비스별 로거 생성과 운영 로그 규칙은 이 패키지의 공개
// 진입점을 통해 맞춥니다.
//
// # 주요 사용 패턴
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
