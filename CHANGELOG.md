# 변경 이력

출시 항목은 실제 Git tag를 기준으로 작성합니다. tag는 소비 저장소가 쓸 변경을 확정해 모듈을 게시할 때만 만들고,
그 사이 병합한 변경은 `미출시`에 누적합니다. 소비 저장소는 게시된 버전을 검증한 뒤 `go.mod`·`go.sum`을 갱신합니다. 발행 PR에서는 다음 release 섹션 하나만 앞에 준비할 수 있으며,
release 섹션은 최신 순으로 배치합니다. 이 문서는 `미출시`와 최근 세 버전 구간만 유지하고 발행 PR에서는 준비하는 release 섹션을 그 첫 번째로 세며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **의존성 최신화:** `openai` 모듈의 `openai-go/v3`를 v3.108.0으로, `transport` 모듈의 `quic-go`를 v0.66.0으로 올렸습니다. `openai`·`postgres`·`transport` 모듈이 요구하는 루트 `shared-go/v2`는 게시된 v2.26.1입니다. CI의 `anchore/sbom-action`은 v0.24.4 커밋, uv는 0.13.10으로 고정했습니다. PostgreSQL 통합 시험의 `postgres:19.0-alpine` digest는 같은 태그의 현재 index로 바꿨습니다. 공개 Go API는 바뀌지 않습니다.
- **비호스트 lint 범위 축소와 CI lint 캐시:** lint 행렬의 호스트 외 설정(plan9·windows·darwin·illumos·wasip1)은 모듈 전체 대신 그 설정을 요구한 파일이 있는 패키지만 검사합니다. 호스트 설정은 그대로 모듈 전체를 검사하고, 커버리지도 371/371로 같습니다. 플랫폼과 무관한 패키지는 호스트 외 플랫폼에서 더 이상 타입 검사하지 않습니다. `module-gate`는 lint 행렬 캐시를 `actions/cache` v6.1.0으로 복원하고, main push에서만 저장합니다. 빈 캐시에서 `make lint MODULES=.`는 로컬 측정으로 90.1초에서 59.8초가 됐습니다.
- **릴리스 게이트 lint 통합:** `release-gate.sh`가 선택한 모듈의 lint를 한 번에 실행하고 각 모듈은 `go build`로 빌드합니다. 단독 `make build`는 계속 lint를 먼저 실행합니다.

## sqlite/v1.0.3 - 2026-11-28

- **적체 중 inbox claim 계획 고정(`sqlite`):** `sqlitestore`의 `claim_candidates.sql`과 `inbox_ready_snapshot.sql`이 `heads`의 각 head를 rowid로 찾도록 `CROSS JOIN`과 `NOT INDEXED`로 조인 순서와 접근 경로를 고정했습니다. v1.0.2의 통계를 pending 행이 없을 때 만들면 `status = 'pending'` 부분 인덱스가 빈 것으로 기록되고, planner는 head마다 그 인덱스를 다시 훑는 계획을 골랐습니다. 표 전체 행 수는 거의 그대로여서 한 시간 주기 `PRAGMA optimize`도 통계를 다시 만들지 않았습니다. 운영 통계를 옮긴 fixture에서 pending 5,000건(ordering key 300개)의 claim 후보 조회는 5.6초에서 1.6ms로, key마다 한 건인 5,000건은 47초에서 5.9ms로 줄었습니다. 통계가 없을 때와 적체 상태에서 새로 분석했을 때도 ms 단위입니다. 결과 행과 순서는 같습니다.

## sqlite/v1.0.2 - 2026-11-28

- **SQLite 통계 갱신(`sqlite`):** `sqlitedb.Open`은 쓰기 연결에서 `PRAGMA optimize=0x10002`를 실행해 통계가 없는 표를 분석합니다. `Migrate`는 마이그레이션을 커밋한 뒤 같은 write gate 안에서 `PRAGMA optimize=0x10012`를 실행하고, owner의 유지보수 루프도 한 시간마다 같은 문장을 실행합니다. 0x10000 비트는 모든 표의 크기를 확인하므로, reader 풀만 조회하는 표도 10배 넘게 커지거나 줄면 다시 분석합니다. 기본 `PRAGMA optimize`는 이 연결의 planner가 통계를 쓴 표만 확인해 그런 표를 놓칩니다. 0x10 비트는 분석 범위를 제한합니다. 모든 실행은 write gate를 거치고, 행이 없는 표에는 통계를 쓰지 않습니다. 통계가 생기면 planner가 실제 행 수와 분포를 보고 계획을 고릅니다. 대표 행을 넣은 시험 fixture에서는 chat-bot-go-kakao의 `classify_inbox.sql`이 값이 하나뿐인 `scope` 인덱스를 전부 훑던 계획에서 rowid 검색으로 바뀌었습니다. `Open`과 `Migrate`는 optimize가 실패하면 오류를 돌려주고, 주기 실행이 실패하면 경고만 기록합니다. `Attach`는 optimize를 실행하지 않습니다.
- `sqlitestore`의 `manual_review_inbox.sql`·`apply_inbox_recovery.sql`에서 검색형 `CASE`를 `iif`로 바꿨습니다. `manual_review_inbox.sql`은 바이트코드가 같습니다. `apply_inbox_recovery.sql`의 `terminal_at`은 바인딩 값만 쓰는 식이라 계산이 문장당 한 번 실행되는 `Once` 블록으로 옮겨진 점만 다릅니다.

## sqlite/v1.0.1 - 2026-11-28

- **마이그레이션 FK 연쇄 삭제 방지:** `sqlitedb.Migrate`가 `foreign_keys=ON`인 transaction에서 SQL을 실행해, 부모 표를 다시 만드는 마이그레이션(새 표 생성 → 복사 → `DROP TABLE` → 이름 변경)이 `ON DELETE CASCADE` 자식 행을 지웠습니다. 이제 SQLite 공식 절차대로 연결의 `foreign_keys`를 끈 뒤 transaction을 열고, 커밋 전에 `foreign_key_check`가 비어 있는지 확인합니다. 위반이 있으면 롤백하고 `ErrForeignKeyViolation`을 돌려줍니다. 성공·실패·panic 뒤에는 `foreign_keys`를 다시 켜 읽어 확인하고, 확인하지 못한 연결은 버려 새 연결이 DSN 설정으로 다시 열리게 합니다. 대기 마이그레이션이 없으면 FK 검사를 하지 않습니다. 마이그레이션 안에서는 `ON DELETE CASCADE`·`SET NULL` 같은 FK 동작도 일어나지 않으므로, 그 동작에 기대던 데이터 마이그레이션은 이제 `ErrForeignKeyViolation`으로 롤백됩니다.
- **쓰기 우선순위:** `WithWritePriority(ctx)`로 표시한 `WriteTx`·`Migrate`는 대기 중인 일반 쓰기보다 먼저 쓰기 연결을 받습니다. 같은 등급은 도착 순이고, 기다리다 context가 끝난 호출은 대기열에서 빠집니다. PostgreSQL `pgxdb.WithControlAcquire`가 정산에 주던 우선 획득을 단일 writer에서 되살립니다. 우선순위는 같은 프로세스 안에서만 적용되며, `Writer()`를 직접 쓰는 호출과 다른 프로세스의 `Attach` 쓰기는 대기열을 거치지 않습니다.
- **닫을 때 대기 제거:** owner `Close`가 TRUNCATE checkpoint에서 `Attach` 프로세스의 reader를 busy timeout만큼 기다린 뒤 `ErrCheckpointIncomplete`를 돌려줬습니다. 이제 기다리지 않고 TRUNCATE를 시도하며, 다른 프로세스의 연결이 WAL을 쓰고 있으면 가능한 frame만 옮기고 WAL을 남긴 채 경고를 기록합니다. 남은 WAL은 다음 `Open`이 이어서 씁니다. 같은 프로세스의 읽기 연결이 아직 열려 있으면 기다리지 않고 `ErrCheckpointIncomplete`를 돌려줍니다. checkpoint에 쓴 연결은 busy timeout을 0으로 바꿨으므로 다시 쓰지 않고 버립니다. 공개 `Checkpoint`의 동작은 같습니다.
