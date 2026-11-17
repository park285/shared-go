// Package logging은 slog 구조화 로깅·민감값 마스킹·파일 보관·추적 상관관계를 제공한다.
// 호출자의 이벤트·메시지·속성에 컨텍스트의 request_id·job_id·runtime·component를 병합한다.
// job_id는 RunOperation이 컨텍스트에 없을 때 새로 만들어 채운다.
// 기록 헬퍼(Debug·Info·Warn·Error·Log·LogAndWrapError·RunOperation)는 logger가 nil이면
// 기록 시점의 slog.Default()에 기록한다.
//
// credential 키 규칙은 이 패키지가 소유하고, 개인 식별자 키(예: room, sender)는 서비스가
// Options.PrivacyKeys로 넘긴다. 내장 privacy 키는 없으므로 room_id·user_id처럼 넘기지 않은 키는
// 보존된다. 민감 키는 LogValue 실행 전에 가리며, 일반 값은 slog scalar·nil·group과
// 명시적 error 진단만 허용한다. map·slice·구조체·일반 pointer·marshaler는 탐색하지 않고
// 고정 차단 사유로 대체한다. LogValuer 결과도 같은 정책을 적용한다.
// group 깊이는 WithGroup과 이름 없는 group을 포함해 8까지 허용하며 초과 부분은 차단한다.
// 빈 WithGroup 이름은 깊이를 늘리지 않는다. 해소 실패와 미지원 타입은 서로 다른 사유를 남긴다.
// error·문자열·메시지는 기존 credential 패턴으로 정제하므로 무표식 비밀까지 탐지하지는 않는다.
// 서비스는 안정적인 code·type과 승인된 설명, 최소 필드의 typed group을 우선한다.
// 보존·마스킹 키 표는 README가 소유한다.
package logging
