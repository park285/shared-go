// Package logging provides shared structured logging helpers for runtime
// services.
//
// # What this package does
//
// 이 패키지는 slog 기반 구조화 로깅을 공통화합니다. 호출부는 event, message,
// slog.Attr를 전달하고, 패키지는 context에 전파된 request_id, job_id, runtime,
// component 값을 자동으로 병합합니다.
//
// operation lifecycle 로깅, 민감값 마스킹 handler, 파일 로깅과 압축 archive,
// OpenTelemetry trace/span 상관관계 주입도 이 패키지의 책임입니다. 서비스별
// 로거 생성과 운영 로그 규칙은 이 패키지의 공개 진입점을 통해 맞춥니다.
//
// # 외부 surface (public API)
//
//   - Debug, Info, Warn, Error, Log: ContextAttrs를 병합해 slog 레벨별 로그를 남깁니다.
//   - WithRequestID, WithJobID, WithRuntime, WithComponent: context에 공통 로그 값을 저장합니다.
//   - RequestIDFromContext, JobIDFromContext, RuntimeFromContext, ComponentFromContext: context에서 공통 로그 값을 읽습니다.
//   - ContextAttrs: context 값을 slog.Attr 목록으로 변환합니다.
//   - Event, Runtime, Component, Operation, RequestID, JobID, DurationMS, SinceMS, ErrorAttrs: 표준 로그 attr helper입니다.
//   - OperationOptions, RunOperation: start/success/failure lifecycle 로그와 duration/error attr를 자동화합니다.
//   - NewID: 정상 경로에서 "<sanitized_prefix>_<unixMillis>_<hex>" 형식의 correlation ID를 생성합니다.
//     단, rand.Read 실패 시 "<sanitized_prefix>_<unixNano>" 형식으로 폴백합니다.
//   - Config, ParseLevel, NewLogger, NewTestLogger, NewTestLoggerWithOutput: 로거 설정과 생성 진입점입니다.
//   - EnableFileLogging, EnableFileLoggingWithLevel, EnableFileLoggingWithOTel: 콘솔/파일 로깅, archive, OTel 상관관계를 구성합니다.
//   - OTelHandler, NewOTelHandler: 활성 span의 trace_id와 span_id를 record에 추가합니다.
//   - SanitizeHandler, NewSanitizeHandler: token, password, key 계열 값을 로그 출력 전에 마스킹합니다.
//
// # 주요 사용 패턴
//
//	ctx = logging.WithJobID(ctx, logging.NewID("ingest"))
//	err := logging.RunOperation(ctx, logger, logging.OperationOptions{
//	    Name: "ingest.batch",
//	}, func(ctx context.Context) error {
//	    return process(ctx)
//	})
//
//	logging.Info(ctx, logger, "youtube.poll.started", "poll started",
//	    logging.Runtime("youtube-producer"),
//	    logging.Component("poller"),
//	)
//
// # 내부 helper 정책
//
// sanitizeIDPrefix, sanitizedIDPrefixRune, errorType, withString,
// stringFromContext, operationContextWithJobID, eventOrDefault, logMessage,
// apply/ensure/archive 계열 helper는 패키지 내부 composition 전용입니다. 외부
// 호출부는 NewID, With/FromContext 쌍, RunOperation, EnableFileLogging 계열을
// 사용합니다. operationContextWithJobID가 NewID와 WithJobID를 합성하므로 별도
// NewOperationID alias는 제공하지 않습니다.
package logging
