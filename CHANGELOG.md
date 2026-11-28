# 변경 이력

출시 항목은 실제 Git tag를 기준으로 작성합니다. tag는 소비 저장소가 쓸 변경을 확정해 모듈을 게시할 때만 만들고,
그 사이 병합한 변경은 `미출시`에 누적합니다. 소비 저장소는 게시된 버전을 검증한 뒤 `go.mod`·`go.sum`을 갱신합니다. 발행 PR에서는 다음 release 섹션 하나만 앞에 준비할 수 있으며,
release 섹션은 최신 순으로 배치합니다. 이 문서는 `미출시`와 최근 세 버전 구간만 유지하고 발행 PR에서는 준비하는 release 섹션을 그 첫 번째로 세며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **비호스트 lint 범위 축소와 CI lint 캐시:** lint 행렬의 호스트 외 설정(plan9·windows·darwin·illumos·wasip1)은 모듈 전체 대신 그 설정을 요구한 파일이 있는 패키지만 검사합니다. 호스트 설정은 그대로 모듈 전체를 검사하고, 커버리지도 371/371로 같습니다. 플랫폼과 무관한 패키지는 호스트 외 플랫폼에서 더 이상 타입 검사하지 않습니다. `module-gate`는 lint 행렬 캐시를 `actions/cache` v6.1.0으로 복원하고, main push에서만 저장합니다. 빈 캐시에서 `make lint MODULES=.`는 로컬 측정으로 90.1초에서 59.8초가 됐습니다.
- **릴리스 게이트 lint 통합:** `release-gate.sh`가 선택한 모듈의 lint를 한 번에 실행하고 각 모듈은 `go build`로 빌드합니다. 단독 `make build`는 계속 lint를 먼저 실행합니다.

## sqlite/v1.0.1 - 2026-11-28

- **마이그레이션 FK 연쇄 삭제 방지:** `sqlitedb.Migrate`가 `foreign_keys=ON`인 transaction에서 SQL을 실행해, 부모 표를 다시 만드는 마이그레이션(새 표 생성 → 복사 → `DROP TABLE` → 이름 변경)이 `ON DELETE CASCADE` 자식 행을 지웠습니다. 이제 SQLite 공식 절차대로 연결의 `foreign_keys`를 끈 뒤 transaction을 열고, 커밋 전에 `foreign_key_check`가 비어 있는지 확인합니다. 위반이 있으면 롤백하고 `ErrForeignKeyViolation`을 돌려줍니다. 성공·실패·panic 뒤에는 `foreign_keys`를 다시 켜 읽어 확인하고, 확인하지 못한 연결은 버려 새 연결이 DSN 설정으로 다시 열리게 합니다. 대기 마이그레이션이 없으면 FK 검사를 하지 않습니다. 마이그레이션 안에서는 `ON DELETE CASCADE`·`SET NULL` 같은 FK 동작도 일어나지 않으므로, 그 동작에 기대던 데이터 마이그레이션은 이제 `ErrForeignKeyViolation`으로 롤백됩니다.
- **쓰기 우선순위:** `WithWritePriority(ctx)`로 표시한 `WriteTx`·`Migrate`는 대기 중인 일반 쓰기보다 먼저 쓰기 연결을 받습니다. 같은 등급은 도착 순이고, 기다리다 context가 끝난 호출은 대기열에서 빠집니다. PostgreSQL `pgxdb.WithControlAcquire`가 정산에 주던 우선 획득을 단일 writer에서 되살립니다. 우선순위는 같은 프로세스 안에서만 적용되며, `Writer()`를 직접 쓰는 호출과 다른 프로세스의 `Attach` 쓰기는 대기열을 거치지 않습니다.
- **닫을 때 대기 제거:** owner `Close`가 TRUNCATE checkpoint에서 `Attach` 프로세스의 reader를 busy timeout만큼 기다린 뒤 `ErrCheckpointIncomplete`를 돌려줬습니다. 이제 기다리지 않고 TRUNCATE를 시도하며, 다른 프로세스의 연결이 WAL을 쓰고 있으면 가능한 frame만 옮기고 WAL을 남긴 채 경고를 기록합니다. 남은 WAL은 다음 `Open`이 이어서 씁니다. 같은 프로세스의 읽기 연결이 아직 열려 있으면 기다리지 않고 `ErrCheckpointIncomplete`를 돌려줍니다. checkpoint에 쓴 연결은 busy timeout을 0으로 바꿨으므로 다시 쓰지 않고 버립니다. 공개 `Checkpoint`의 동작은 같습니다.

## sqlite/v1.0.0 - 2026-11-28

- **새 `sqlite` 모듈(`github.com/park285/shared-go/sqlite`):** `sqlitedb`가 CGO 없는 `ncruces/go-sqlite3` v0.38.1로 SQLite 파일 하나를 엽니다. `<경로>.lock` flock으로 한 프로세스만 `Open`하고, 쓰기 연결 1개(`BEGIN IMMEDIATE` `WriteTx`, 콜백이 panic해도 롤백)와 `query_only` 읽기 풀, WAL·`synchronous=FULL`·`foreign_keys=ON`, 내장 마이그레이션 원장(`sqlitedb_migrations`), 결과 불명 COMMIT의 `ErrCommit`을 제공합니다. 주기 WAL checkpoint는 writer를 기다리게 하지 않는 PASSIVE이고 닫을 때만 TRUNCATE합니다. `Attach`는 잠금 파일 없이 기존 파일에 붙는 운영 명령용입니다.
- `sqlitestore`는 irisdurable inbox·nonce 계약의 SQLite 구현입니다. 만료된 lease를 자동 회수하지 않으며(`pgstore`의 `InboxExplicitRecovery=true`에 해당) 소비자가 `ApplyInboxRecovery`로 정리합니다. 커밋이 실패하거나 롤백되면 prune·claim 해제 건수는 0입니다.
- 릴리스 workflow·번들 스크립트·계약 모듈 목록에 `sqlite/v1.*`를 추가했습니다. 첫 소비자는 chat-bot-go-kakao와 hololive-bot입니다.

## v2.26.1 - 2026-11-26

- **평문 서식 모사 퇴역:** `kakaoformat.Render`가 강조·기울임을 수학용 Unicode 알파벳이나 괄호로 바꾸고 결합 취소선을 붙이던 처리를 제거했습니다. 제목 장식, 단계별 특수 글머리표, 인용 장식, 코드 박스와 표의 열별 장식도 제거했습니다. 평문은 원래 문자와 URL을 유지하고, 목록·할 일 상태·인용·표는 일반 문자로 구조를 보존합니다. 코드 본문은 원문 공백과 줄바꿈을 보호하며 넓은 fence 안의 내부 fence·언어 이름을 재작성하지 않습니다. 위·아래 첨자 등 수식의 의미 표기는 유지합니다.
- `Render` 등 공개 API와 소비자의 방 유형·RichText 선택 정책은 유지합니다. Markdown 파서, CJK 경계 처리, 코드 범위 탐색과 이스케이프 API는 평문·리터럴 보호에 계속 필요합니다. 네이티브 RichText는 원문을 Iris에 전달하며 이 평문 변환을 먼저 적용하지 않습니다. 새 출력 계약은 소비자가 다음 릴리스로 pin을 갱신하거나 변경된 shared-go 소스를 workspace에서 사용할 때 적용됩니다.
- 원문 Markdown만 저장하는 소비자는 재시도 때 최종 본문이 달라질 수 있으므로, 새 렌더러 적용과 롤백 전에 기존 미완료 요청을 이전 버전의 확인·복구 절차로 처리합니다. cbgk의 inbox·답변 outbox·멘션 claim 전환 조건은 README의 연결 문서를 따릅니다.
