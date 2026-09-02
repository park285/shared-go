// Package irisdurable은 Iris webhook 수신·응답 경로의 durability 계약을 스택 공통으로 정의한다.
//
// chat-bot-go-kakao, twentyq-bot, hololive-bot의 PostgreSQL inbox·reply outbox·reissue 구현이
// 공유해야 하는 어휘(admission 결과, reply 상태), typed 상수(Iris admission 보존, 자동 replay
// 지평), bounded reissue ladder를 이 패키지가 소유한다. 저장소 구현과 도메인 큐는 각 봇에 남고,
// 하위 패키지 contracttest가 각 구현이 이 계약을 지키는지 검증한다.
//
// iris-client-go의 webhook.MessageAdmitter·webhook.SetOnceNonceStore seam은 그대로 두고, 이
// 패키지는 그 seam 뒤의 저장소 계약만 다룬다. 두 모듈 사이에 import 의존은 없으므로 reissue 세대
// 상한과 파생 규칙은 호출자가 iris-client-go 값을 ReissueLadder에 넘겨 한 곳에서만 정의되게 한다.
package irisdurable
