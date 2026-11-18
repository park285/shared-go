# InboxClaim: 누가 다음 메시지를 처리할 수 있는가

동기화 커밋: 9d61ad2dd28dacbb44ae93ed7550aa4c917e4f71

**결과:** 기본 설정의 여섯 속성이 모두 통과했다. 임대 만료만으로 현재 토큰이 폐기되지는 않는다. 다른 호출이 같은 행을 재획득한 뒤에는 이전 토큰으로 갱신하거나 완료할 수 없다.

nonce 선점은 `insert_nonce.sql`의 조건부 upsert 한 문장이다. 반면 inbox에서는 서버가 같은 토큰으로 여러 키를 확보한 뒤 클라이언트가 그중 일부만 읽고 실패할 수 있다. 반납하기 전에 다른 처리자가 행을 다시 잡거나 기존 처리자가 완료할 수도 있다. `inbox_heads_test.go`와 `inbox_batch_test.go`의 실제 DB 사례를 대체하지 않고 이런 순서의 조합을 TLC로 탐색했다. `chat-bot-go-kakao/internal/storage/webhookinbox/pgstore.go`는 실제로 결과 중 계속 처리할 ID만 보존하며, `twentyq-bot/internal/twentyq/pgstate/webhook_inbox.go`는 단건 claim과 갱신을 이 저장소에 위임한다. 이 검사가 다른 봇의 자체 큐까지 검증하는 것은 아니다.

## 속성과 위반 시 피해

| 속성 | 약속과 이유 |
|---|---|
| `OrderingKeyFIFO` | 같은 키의 앞 메시지가 완료되기 전에 뒷 메시지를 확보하면 안 된다. 같은 방의 명령 순서가 뒤집힐 수 있다. |
| `ReclaimOnlyExpiredImplicit` | 처리 중인 행은 임대가 만료되고 암시적 재획득이 허용될 때만 다시 잡아야 한다. 그렇지 않으면 살아 있는 처리자의 소유권을 빼앗거나 명시적 복구 절차를 건너뛴다. |
| `ReleaseOnlyUnreturned` | 반납 SQL이 성공하면 해당 토큰이 아직 가진 처리 중 행에서 `keep`을 뺀 행만 대기로 돌리고 임대·토큰을 지우며 시도 횟수를 정확히 하나 돌려야 한다. 잘못하면 넘겨준 작업이 다시 실행되거나 실행하지 않은 작업의 재시도 예산이 줄어든다. |
| `CompleteOnlyLiveOwner` | 완료는 현재 토큰과 살아 있는 임대가 모두 필요하다. 그렇지 않으면 이전 처리자가 새 처리자의 작업을 끝내거나 만료된 임대로 완료할 수 있다. |
| `RenewOnlyCurrentOwner` | 갱신이 성공했다면 호출자의 토큰이 해당 행의 갱신 직전 토큰과 같아야 한다. 그렇지 않으면 재획득으로 교체된 처리자도 새 처리자의 임대를 바꿀 수 있다. |
| `ExpiredCurrentOwnerCanRenew` | 만료 후에도 처리 중인 행의 토큰이 교체되지 않았다면 그 claim을 받은 호출자는 갱신할 수 있어야 한다. 만료를 토큰 폐기로 취급하면 정상적인 갱신 후 완료를 거부한다. |

## 코드 대응과 원자성

아래 경로는 `postgres/pgstore/` 기준이다. `sql.go:inboxQuery`가 claim SQL 조각을 조립한다. `releaseExact`, `completionFenced`, `reclaimFenced`, `renewalFenced`는 데이터베이스의 추가 조건이 아니라 성공한 동작의 전후 상태를 검사하는 관측값이다. 갱신이 기존과 같은 만료 시각을 써도 행을 찾았다면 성공이므로 그 토큰을 검사한다. `ExpiredCurrentOwnerCanRenew`는 만료 경계에서 갱신 동작이 가능한지를 검사하며, 호출자가 실제로 갱신할 것이라고 가정하지 않는다.

| 동작 | 코드에서 관찰하는 한 단계 |
|---|---|
| `ClaimBatch` | `inbox_batch.go:Store.ClaimBatch` 또는 `inbox.go:Store.Claim`이 보낸 `queries/claim_inbox.sql` 한 문장의 커밋. `inbox_heads.sql`에서 키별 가장 이른 미종단 행을 구한 **뒤** `inbox_eligible.sql`의 상태·정책·시각 필터와 limit을 적용한다. 선택한 행 모두에 같은 새 토큰·만료 시각을 쓰고 각각 `attempts`를 올린다. |
| `ReturnClaims` | `ClaimBatch`가 호출자에게 claim 슬라이스를 반환하는 순간. 데이터베이스 행은 바뀌지 않는다. `rows.Scan`/`rows.Err` 실패 전까지 쌓인 슬라이스는 호출자에게 아직 보이지 않는다. SQL 반환 순서는 미지정이므로 반환한 접두부의 ID 집합은 빈 집합부터 전체까지 가능하다. 전량 반환은 성공 또는 전량을 읽은 뒤의 오류를 나타낼 수 있다. |
| `ReleaseClaimToken` | `inbox_batch.go:Store.ReleaseClaimToken` → `queries/release_claim_token.sql` 한 문장. 호출자가 계속 처리할 ID 집합을 넘긴다. 반환 행을 모두 계속 처리하는 경우를 다루며, 빈 결과와 일부 결과를 모두 허용한다. 호출이 늦어지거나 일어나지 않을 수도 있다. |
| `Complete` | `inbox.go:Store.Complete` → `queries/complete_inbox.sql`이 한 행을 종단으로 변경한다. 메시지 ID는 고정이고, 토큰과 `lease_until > clock_timestamp()`를 검사한다. `execFenced`는 변경 행이 0이면 `ErrClaimLost`를 반환한다. |
| `RenewInbox` | `inbox.go:Store.RenewInbox` → `queries/renew_inbox_lease.sql` 한 문장. 처리 중인 현재 토큰이면 기존 만료 시각과 무관하게 새 만료 시각을 쓴다. 재획득이 먼저면 토큰 불일치로 0행이고 `execFenced`는 `ErrClaimLost`를 반환한다. |
| `Tick` | DB 시계가 다음 관측 시점에 도달한다. Go나 SQL이 미리 행 상태를 바꾸는 동작이 아니다. |

## 실행과 음성 대조군

shared-go 루트에서 실행한다. 고정 배포물은 `tla2tools` 1.7.4이며 실행 배너는 TLC2 2.19다.

```bash
TLC_WORKERS=2 bash /home/kapu/work/iris-stack/tools/lint/verify-tools.sh tlc verification/tla/InboxClaim/InboxClaim.tla
```

기본 설정은 종료 **0**이었다. 작업자 2개로 6,365개 상태를 생성했고, 고유 상태 1,606개를 탐색해 깊이 10에서 대기열이 비었다. TLC가 보고한 실행 시간은 1초였다.

음성 대조군은 변형마다 `InboxClaim.tla`와 `InboxClaim.cfg`를 같은 임시 디렉터리에 복사한 뒤 아래 동작 하나만 바꾼다. 속성과 관측식은 그대로 두고 위 명령에서 모델 경로만 복사본으로 바꾼다. 종료 12와 해당 속성 이름을 함께 확인한다.

| 속성 | 단일 변경 | 관측한 TLC 결과 |
|---|---|---|
| `OrderingKeyFIFO` | `Ready`에서 `IsHead(i)` 필터 삭제 | 종료 12, 첫 batch가 같은 키 1·2번을 함께 확보해 위반 |
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

- 단일 scope에 이미 커밋된 세 행 `(room,1), (room,2), (other,3)`을 둔다. 모두 즉시 적격인 예약 시각이고 `created_at,id`의 순서가 ID 순서와 같다. 충돌 없는 토큰 2개, 비어 있지 않은 claim 최대 2회, 호출별 limit 1 또는 2, 시각 0·1·2, 임대 길이 2다. 정책은 명시적/암시적 각각 한 초기 상태에서 고정한다.
- DB 문장들은 autocommit으로 서로 겹치지 않고 실행된다. 결과 반환·반납·재획득·갱신·완료 사이의 순서는 열어 둔다. 롤백되거나 아예 커밋하지 않은 claim, 빈 조회, 조건 불일치로 0행인 갱신은 이 모델의 저장 상태를 바꾸지 않는다. SQL 실행 도중의 시계 이동, 트랜잭션 스냅샷·락 대기·`SKIP LOCKED`/최신 행 재검사, 열린 트랜잭션 내의 다중 문장, 동시 admission은 검증하지 않는다.
- 보존 기간·prune·nonce·dedup·`ApplyInboxRecovery`·수동 검토·재예약·workerpool·외부 메시지 전송·프로세스 장애는 모델에 없다. 성공한 반납의 효과를 검증하지 모든 반납이 언젠가 성공한다고 주장하지 않는다. 유한 시간/토큰 소진 때문에 `CHECK_DEADLOCK FALSE`이며 공정성·무기아·정확히 한 번 전달은 검증하지 않는다.
- 동기화 커밋 이후 위 SQL 또는 Go 호출 계약이 바뀌면 먼저 코드의 경계·필터·반환값을 다시 대응시키고, 변형이 실제로 깨지는지와 기본 설정 결과를 갱신한 다음에만 커밋 표기를 옮긴다.
