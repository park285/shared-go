# InboxClaim: 누가 다음 메시지를 처리할 수 있는가

동기화 커밋: 9d61ad2dd28dacbb44ae93ed7550aa4c917e4f71

**결과:** 기본 설정의 여덟 속성이 모두 통과했다. 같은 키에서는 수락 시각이 역행해도 미정산 행을 추월하지 않는다. 서로 다른 키에서는 단건 claim이 나중에 재예약된 head보다 먼저 도래한 head를 고른다. 임대 만료만으로 현재 토큰이 폐기되지는 않지만, 재획득으로 교체된 토큰으로는 갱신하거나 완료할 수 없다.

nonce 선점은 `insert_nonce.sql`의 조건부 upsert 한 문장이다. 반면 inbox에서는 서버가 같은 토큰으로 여러 키를 확보한 뒤 클라이언트가 그중 일부만 읽고 실패할 수 있다. 반납하기 전에 다른 처리자가 행을 다시 잡거나 기존 처리자가 완료할 수도 있다. `inbox_heads_test.go`와 `inbox_batch_test.go`의 실제 DB 사례를 대체하지 않고 이런 순서의 조합을 TLC로 탐색했다. `chat-bot-go-kakao/internal/storage/webhookinbox/pgstore.go`는 실제로 결과 중 계속 처리할 ID만 보존하며, `twentyq-bot/internal/twentyq/pgstate/webhook_inbox.go`는 단건 claim과 갱신을 이 저장소에 위임한다. 이 검사가 다른 봇의 자체 큐까지 검증하는 것은 아니다.

## 속성과 위반 시 피해

| 속성 | 약속과 이유 |
|---|---|
| `OrderingKeyFIFO` | 같은 키의 앞 메시지가 완료되기 전에 뒷 메시지를 확보하면 안 된다. 같은 방의 명령 순서가 뒤집힐 수 있다. |
| `OneProcessingPerPartition` | 같은 키에 `processing` 상태인 행은 최대 하나다. 늦은 커밋이 head를 바꿔 두 처리자에게 동시에 처리권을 주면 안 된다. |
| `ReadyHeadsByPriority` | 선택된 모든 head의 `(available_at, created_at, id)`가 선택되지 않은 적격 head보다 앞선다. 이를 어기면 단건 claim에서 한 방의 재예약이 다른 방을 계속 밀어낼 수 있다. |
| `ReclaimOnlyExpiredImplicit` | 처리 중인 행은 임대가 만료되고 암시적 재획득이 허용될 때만 다시 잡아야 한다. 그렇지 않으면 살아 있는 처리자의 소유권을 빼앗거나 명시적 복구 절차를 건너뛴다. |
| `ReleaseOnlyUnreturned` | 반납 SQL이 성공하면 해당 토큰이 아직 가진 처리 중 행에서 `keep`을 뺀 행만 대기로 돌리고 임대·토큰을 지우며 시도 횟수를 정확히 하나 돌려야 한다. 잘못하면 넘겨준 작업이 다시 실행되거나 실행하지 않은 작업의 재시도 예산이 줄어든다. |
| `CompleteOnlyLiveOwner` | 완료는 현재 토큰과 살아 있는 임대가 모두 필요하다. 그렇지 않으면 이전 처리자가 새 처리자의 작업을 끝내거나 만료된 임대로 완료할 수 있다. |
| `RenewOnlyCurrentOwner` | 갱신이 성공했다면 호출자의 토큰이 해당 행의 갱신 직전 토큰과 같아야 한다. 그렇지 않으면 재획득으로 교체된 처리자도 새 처리자의 임대를 바꿀 수 있다. |
| `ExpiredCurrentOwnerCanRenew` | 만료 후에도 처리 중인 행의 토큰이 교체되지 않았다면 그 claim을 받은 호출자는 갱신할 수 있어야 한다. 만료를 토큰 폐기로 취급하면 정상적인 갱신 후 완료를 거부한다. |

## 코드 대응과 원자성

아래 경로는 `postgres/pgstore/` 기준이다. `sql.go:inboxQuery`가 claim SQL 조각을 조립한다. `releaseExact`, `completionFenced`, `reclaimFenced`, `renewalFenced`는 데이터베이스의 추가 조건이 아니라 성공한 동작의 전후 상태를 검사하는 관측값이다. 갱신이 기존과 같은 만료 시각을 써도 행을 찾았다면 성공이므로 그 토큰을 검사한다. `ExpiredCurrentOwnerCanRenew`는 만료 경계에서 갱신 동작이 가능한지를 검사하며, 호출자가 실제로 갱신할 것이라고 가정하지 않는다.

| 동작 | 코드에서 관찰하는 한 단계 |
|---|---|
| `Init` / `CommitAdmission` | `queries/admit_inbox.sql`은 같은 `(scope, ordering_key)`의 advisory transaction lock을 얻은 CTE 행을 입력으로 받아 INSERT의 기본 ID를 발급한다. 초기 상태는 1번이 커밋되고 2번이 별도 트랜잭션에 삽입된 직후다. 2번은 더 큰 ID를 가지지만 시각은 더 이를 수도 있다. 이 행은 커밋한 뒤의 claim 문부터 보인다. |
| `ClaimBatch` | `inbox_batch.go:Store.ClaimBatch` 또는 `inbox.go:Store.Claim`이 보낸 `queries/claim_inbox.sql` 한 문장의 커밋. `inbox_heads.sql`에서 키별 미종단 행 중 최소 ID를 구한 **뒤** `inbox_eligible.sql`의 상태·정책·시각 필터를 적용한다. `Candidates`는 서로 다른 적격 head에만 `(available_at, created_at, id)`순 한도를 적용한다. `PriorityKept`는 선발 함수와 별개로 선택·미선택 쌍의 예약 시각을 대조하고, 동률이면 수락 시각, 다시 동률이면 ID를 대조한다. `inbox_ready_snapshot.sql`도 같은 head·적격성 식을 쓴다. 선택한 행 모두에 같은 새 토큰·만료 시각을 쓰고 각각 `attempts`를 올린다. |
| `Defer` | `Store.DeferInboxUntil` → `queries/defer_inbox.sql`. 반환된 claim의 ID·메시지·토큰이 현재 처리 행과 같으면 한 번의 시도를 돌려주고 토큰과 임대를 지워 다음 시각에 예약한다. `Tick` 후 다른 키의 이미 도래한 head와 다시 경합한다. |
| `ReturnClaims` | `ClaimBatch`가 호출자에게 claim 슬라이스를 반환하는 순간. 데이터베이스 행은 바뀌지 않는다. `rows.Scan`/`rows.Err` 실패 전까지 쌓인 슬라이스는 호출자에게 아직 보이지 않는다. SQL 반환 순서는 미지정이므로 반환한 접두부의 ID 집합은 빈 집합부터 전체까지 가능하다. 전량 반환은 성공 또는 전량을 읽은 뒤의 오류를 나타낼 수 있다. |
| `ReleaseClaimToken` | `inbox_batch.go:Store.ReleaseClaimToken` → `queries/release_claim_token.sql` 한 문장. 호출자가 계속 처리할 ID 집합을 넘긴다. 반환 행을 모두 계속 처리하는 경우를 다루며, 빈 결과와 일부 결과를 모두 허용한다. 호출이 늦어지거나 일어나지 않을 수도 있다. |
| `Complete` | `inbox.go:Store.Complete` → `queries/complete_inbox.sql`이 한 행을 종단으로 변경한다. 메시지 ID는 고정이고, 토큰과 `lease_until > clock_timestamp()`를 검사한다. `execFenced`는 변경 행이 0이면 `ErrClaimLost`를 반환한다. |
| `RenewInbox` | `inbox.go:Store.RenewInbox` → `queries/renew_inbox_lease.sql` 한 문장. 처리 중인 현재 토큰이면 기존 만료 시각과 무관하게 새 만료 시각을 쓴다. 재획득이 먼저면 토큰 불일치로 0행이고 `execFenced`는 `ErrClaimLost`를 반환한다. |
| `Tick` | DB 시계가 다음 관측 시점에 도달한다. Go나 SQL이 미리 행 상태를 바꾸는 동작이 아니다. |

## 실행과 음성 대조군

shared-go 루트에서 실행한다. 고정 배포물은 `tla2tools` 1.7.4이며 실행 배너는 TLC2 2.19다.

```bash
TLC_WORKERS=2 bash scripts/ci/verify-tools.sh tlc verification/tla/InboxClaim/InboxClaim.tla
```

`a39addb0590552e916cd450e5ddec5e03f345533`의 기존 설정은 종료 0(생성 6,365개·고유 1,606개·깊이 10)이었지만 수락 시각 역행과 늦은 커밋을 표현하지 못했다. 이를 표현한 중간 모델도 종료 0(생성 36,126개·고유 8,280개·깊이 11)이었지만 재예약·키 간 선발은 검사하지 않았다. `Defer`와 선발 순위를 반영한 기본 설정은 작업자 2개로 상태 102,388개를 생성하고 고유 상태 23,000개를 깊이 12까지 탐색했다. 남은 상태 0개, 종료 **0**이었다.

PostgreSQL 18.6에서 `eligibility_test.go:TestInboxClaimDoesNotPassProcessingHeadAfterClockRollback`은 A 수락 후 B의 트랜잭션에서만 `clock_timestamp()`를 A보다 1µs 이르게 돌린다. B가 안 보일 때 별도 연결로 A를 확보하고 B를 커밋했다. 기존 SQL은 두 번째 claim에 B를 반환했다. 수정 후에는 준비 수와 두 번째 claim 모두 0이고, A 정산 후에야 B를 확보했다.

첫 키와 후속 키를 별도로 검증했다. `inbox_heads.sql`의 첫 번째 또는 재귀 분기 하나만 시각순으로 되돌리면 해당 사례에서만 `second`가 앞 행 정산 전에 반환됐다. `TestAdmitWaitsForOrderingLockBeforeTakingSequenceNumber`는 첫 세션이 삽입 전에 잠근 상태에서 `pg_blocking_pids`로 두 번째 admission의 대기를 관찰한다. 이때 `is_called`는 거짓이고 첫 세션의 수락·커밋 뒤에야 대기자가 더 큰 ID를 받았다. `nextval`을 잠금보다 앞선 CTE에서 실행한 변이는 대기 중 `is_called`가 참이어서 실패했다.

`TestDeferredInboxHeadYieldsToEarlierDueKey`에서 서로 다른 키 A·B 중 A를 확보해 B보다 늦게 재예약한 뒤 두 head가 모두 준비된 시점에 단건 claim했다. ID순 변이는 다시 A를 반환했지만 원본은 B를 반환했다. `TestInboxClaimBreaksTiesBetweenDueKeys`는 예약 시각이 같을 때 더 먼저 수락된 B를, 수락 시각까지 같으면 ID가 작은 A를 요구한다. `created_at`을 생략한 SQL 변이는 첫 사례에서 A를, 마지막 ID를 내림차순으로 바꾼 변이는 둘째 사례에서 B를 반환해 각각 실패했다.

음성 대조군은 변형마다 `InboxClaim.tla`와 `InboxClaim.cfg`를 같은 임시 디렉터리에 복사한 뒤 아래 동작 하나만 바꾼다. 속성과 관측식은 그대로 두고 위 명령에서 모델 경로만 복사본으로 바꾼다. 종료 12와 해당 속성 이름을 함께 확인한다.

| 속성 | 단일 변경 | 관측한 TLC 결과 |
|---|---|---|
| `OrderingKeyFIFO` | `Ready`에서 `IsHead(i)` 필터 삭제 | 종료 12, 2번 커밋 뒤 첫 batch에서 1·2번 동시 확보 |
| `OrderingKeyFIFO` | `Precedes(j,i)`를 `(createdAt,id)` 사전식 비교로 복원 | 종료 12, `createdAt[2] < createdAt[1]`일 때 `CommitAdmission → ClaimBatch`로 1번보다 먼저 2번 확보 |
| `OneProcessingPerPartition` | 같은 시각 정렬 복원 사본에서 이 속성만 선택 | 종료 12, `ClaimBatch(1번) → CommitAdmission(2번) → ClaimBatch(2번)`. 시각 이동이나 임대 만료 없이 두 행 모두 `processing` |
| `ReadyHeadsByPriority` | `HigherPriority(j,i)` 정의 전체를 `j < i`로 대체 | 기존 관측식은 같은 비교기를 써서 변이도 종료 0이었다. 독립 관측식은 종료 12. `ClaimBatch(1번) → ReturnClaims → Defer → Tick → ClaimBatch(1번)`에서 예약 시각 0인 3번보다 시각 1인 1번을 선발했다. |
| `ReclaimOnlyExpiredImplicit` | `Eligible`에서 `~explicitRecovery` 삭제 | 종료 12, 명시적 모드인데 만료 직후 두 번째 호출이 같은 행을 확보해 위반 |
| `ReclaimOnlyExpiredImplicit` | `Eligible`에서 `leaseUntil <= now` 삭제 | 종료 12, 첫 임대가 살아 있는 즉시 두 번째 호출이 다시 확보해 위반 |
| `ReleaseOnlyUnreturned` | `matches`에서 `i ∉ keep` 삭제 | 종료 12, 넘긴 행까지 대기로 되돌려 위반 |
| `ReleaseOnlyUnreturned` | `matches`에서 토큰 비교 삭제 | 종료 12, 두 batch가 다른 행을 잡은 뒤 한 호출이 다른 호출의 행까지 반납해 위반 |
| `ReleaseOnlyUnreturned` | 반납 시 `attempts`를 그대로 둠 | 종료 12, 아무 행도 넘기지 못했는데 반납 후 시도 횟수 1이 남아 위반 |
| `CompleteOnlyLiveOwner` | `Complete`의 토큰 비교 삭제 | 종료 12, 다시 확보한 행을 이전 토큰이 완료해 위반 |
| `CompleteOnlyLiveOwner` | `Complete`의 `leaseUntil > now` 삭제 | 종료 12, 재획득 전이라도 만료된 토큰이 완료해 위반 |
| `RenewOnlyCurrentOwner` | `RenewInbox`의 토큰 비교 삭제 | 종료 12, 첫 토큰을 받은 뒤 만료·재획득을 거쳐 첫 토큰의 갱신이 성공해 위반 |
| `ExpiredCurrentOwnerCanRenew` | `RenewInbox`에 `leaseUntil > now` 추가 | 종료 12, 첫 토큰을 받은 뒤 교체 없이 만료 경계에 도달하자 갱신할 수 없어 위반 |

## 검토로 철회된 가정

“임대가 만료되면 같은 토큰도 갱신할 수 없다”는 가정을 철회했다. [소비자 계약 테스트 `TestCompleteRequiresTheMessageIdAndALiveLease`](https://github.com/park285/shared-go/blob/9d61ad2dd28dacbb44ae93ed7550aa4c917e4f71/postgres/pgstore/consumer_surface_test.go#L197-L221)는 [완료에 임대 검사를 도입한 커밋 `65cc29d`](https://github.com/park285/shared-go/commit/65cc29d)부터 만료 직후의 완료는 `ErrClaimLost`, 재획득 전 같은 토큰의 갱신과 이후 완료는 성공이라고 명시한다. 만료는 완료 권한을 일시 중단하고 암시적 재획득을 허용할 뿐이다. 재획득이 먼저 새 토큰을 쓰면 이전 토큰의 갱신과 완료가 모두 거부된다. 따라서 교체 전 갱신은 제품 결함이 아니다.

## 경계와 동기화

- 단일 scope에 `(room,1), (room,2), (other,3)`을 둔다. 1·3번은 커밋됐고 2번은 삽입됐으나 아직 보이지 않는다. 1번 수락 시각의 상대값은 1, 2번은 0·1·2로 역행·동률·증가를 모두 허용한다. 이 상대 순서와 별개로 초기 `available_at`은 모두 0이고 재예약은 `now + 1`이다. 충돌 없는 토큰 2개, 비어 있지 않은 claim 최대 2회, 호출별 limit 1 또는 2, 시계 0·1·2와 임대 길이 2다. 명시적/암시적 재획득 정책은 실행 중 고정한다.
- ID는 잠금 안에서 발급된 증가 순번의 상대값이다. PostgreSQL 기본 `bigserial`(양의 증가폭, `CACHE 1`, `NO CYCLE`), 실행 중 ID 강제 입력·`setval` 없음이 전제다. 커밋되지 않은 2번과 claim 사이의 문장 가시성을 다루지만, 시퀀스 내부 동작·번호의 구멍·락 경합·문장 내부의 MVCC/`SKIP LOCKED` 재검사를 모델링하지 않는다. SQL 결과 반환·반납·재획득·갱신·완료 사이의 순서는 열어 둔다. 비커밋 claim, 빈 조회, 조건 불일치로 0행인 갱신은 저장 상태를 바꾸지 않는다.
- 보존 기간·prune·nonce·dedup·`ApplyInboxRecovery`·수동 검토·임의의 재예약 간격·workerpool·외부 메시지 전송·프로세스 장애는 모델에 없다. 순서가 뒤집힌 기존 처리 행이나 만료 복구로 다시 `pending`이 된 재시도 행도 초기 상태에 없다. [전환·롤백 검사](../../../README.md#inbox-수락-순서)는 이 경우를 실제 DB에서 판별한다. 두 방향 모두 미정산 행에서 시각순 head 2와 ID순 head 1을 반환했고, 현재 구현으로 재시도 행을 정산하면 0행이었다. 성공한 반납의 효과를 검증하지 모든 반납이 언젠가 성공한다고 주장하지 않는다. 유한 시간/토큰 소진 때문에 `CHECK_DEADLOCK FALSE`이며 공정성·무기아·정확히 한 번 전달은 검증하지 않는다.
- 동기화 커밋 이후 위 SQL 또는 Go 호출 계약이 바뀌면 먼저 코드의 경계·필터·반환값을 다시 대응시키고, 변형이 실제로 깨지는지와 기본 설정 결과를 갱신한 다음에만 커밋 표기를 옮긴다.
