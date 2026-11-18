# 원형 큐의 FIFO 보존

어떤 용량과 유한한 `Push`·`Pop` 호출열에서도 원형 큐의 응답과 남은 원소열은 같은 용량의 FIFO 목록과 정확히 일치한다. 따라서 꽉 찬 큐는 새 작업을 거절하고, 머리가 몇 번 순환하더라도 승인된 작업을 잃거나 복제하거나 순서를 바꾸지 않는다. 이 규칙이 깨지면 워커 작업이나 종료 콜백이 누락되거나 두 번 실행될 수 있다.

## 대상과 대응

대상: `pkg/workerpool/bounded_queue.go`의 `newBoundedQueue`, `Push`, `Pop`, `Len`.

| Go 상태·연산 | `BoundedQueue.lean`의 대응 |
| --- | --- |
| `len(items)`, `head`, `size` | `Queue α capacity`의 고정 용량, `head`, `size`. `items`는 물리 인덱스에서 값을 읽는 함수다. 유효 인덱스 밖의 함수값은 관찰하지 않는다. |
| `make([]T, max(capacity, 0))` | 음수를 0으로 정규화한 용량에서 `newBoundedQueue zero`가 모든 슬롯을 영값으로 채운다. `emptyRepresents`가 빈 FIFO와의 대응을 세운다. |
| `Push(value)` 한 번 | `push`: 가득 차면 상태 불변과 `false`, 아니면 `(head + size) % capacity`에 기록한 뒤 크기를 하나 늘리고 `true`. 참조 연산 `fifoPush`는 목록 끝에만 추가한다. |
| `Pop()` 한 번 | `pop`: 비면 상태 불변과 `(zero, false)`, 아니면 머리 값을 읽고 그 슬롯을 지운 뒤 머리를 한 칸 순환시키고 크기를 하나 줄인다. `fifoPop`은 목록 머리만 제거한다. |
| 크기와 남은 내용 | `Represents`는 크기가 용량 이하이고 목록 길이와 같으며 모든 유효 논리 인덱스의 값이 일치함을 요구한다. `traceRefines`는 각 응답과 마지막 상태에 대해 이 대응을 임의 길이 호출열로 확장한다. |

## 가정과 제외 범위

- 증명은 자연수 용량·유한 호출열 길이에 상한을 두지 않는다. Go와의 대응에는 할당 성공과 인덱스 덧셈의 `int` 표현 가능성이 필요하다. 제품에서 `pkg/workerpool/managed_pool.go`의 작업 및 종료 큐 용량 검사는 모두 최대 1,048,576이므로 유효한 `head + size`는 32비트 `int`에도 들어간다.
- 호출 사이의 논리적 FIFO를 다룬다. 잠금, 워커 실행, 종료 콜백, 스케줄링의 공정성, 메모리 할당 실패, `Front`·`At`·`Drain` 메서드 자체는 증명하지 않는다.
- `pkg/workerpool/bounded_queue_trace_fuzz_test.go`는 실제 큐와 별도 슬라이스 FIFO에서 같은 호출열을 수행하고 승인 여부, 매 `Pop`의 값과 성공 여부, 매 단계의 `Len`, 마지막 유효 `At` 값들을 대조한다. 영값 원소와 같은 값의 반복도 허용한다. 테스트의 `int8` 용량은 탐색 비용 제한이지 정리의 가정이 아니다.

## 반증 실험

저장소 밖의 새 복사본에서 `pop`의 `(q.head + 1) % capacity`만 `(q.head + 2) % capacity`로 바꾸었다. 같은 `lake build`가 `popRefines`의 후속 슬롯 동등식에서 종료 코드 **1**로 실패했다. 용량 2에서 `Push(1), Push(2), Pop(), Pop()`을 하면 변형은 지운 첫 번째 슬롯을 다시 읽어 두 번째 `Pop`에서 2 대신 0을 돌려준다. 원본 빌드는 종료 코드 **0**이었다.

## 실행

`shared-go` 저장소 루트에서 실행한다. Go 명령은 상위 작업 트리 그룹의 `go.work`를 사용한다.

```sh
(cd verification/lean/BoundedQueue && bash /home/kapu/work/iris-stack/tools/lint/verify-tools.sh lake build)
go test -run '^FuzzBoundedQueueFIFO$' ./pkg/workerpool
go test -run '^$' -fuzz '^FuzzBoundedQueueFIFO$' -fuzztime=5s -parallel=2 ./pkg/workerpool
```

동기화 커밋: 9d61ad2dd28dacbb44ae93ed7550aa4c917e4f71
