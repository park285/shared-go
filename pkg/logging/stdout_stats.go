package logging

import "sync/atomic"

var (
	stdoutDropped   atomic.Uint64
	stdoutTruncated atomic.Uint64
)

// StdoutStats는 파일 로거의 표준 출력 복사 결과를 프로세스 수명 동안 집계한다.
// Dropped는 큐 포화·닫힘, 대상 오류·부분 쓰기를 센다.
// Truncated는 크기를 제한한 JSON 쓰기 성공만 세며 실패는 Dropped에만 반영한다.
// 종료 진단·파일 쓰기·콘솔 전용 로거는 제외하고 진행 중인 쓰기는 아직 집계하지 않는다.
type StdoutStats struct {
	Dropped   uint64
	Truncated uint64
}

// SnapshotStdout은 각자 독립적으로 읽은 단조 증가 카운터를 반환하며 초기화하지 않는다.
// Close의 비동기 배출 대기는 최대 2초지만 이후에도 쓰기가 끝날 수 있다.
// 카운터와 종료 요약은 확정된 결과만 나타내며 대기 중 I/O의 취소를 뜻하지 않는다.
func SnapshotStdout() StdoutStats {
	return StdoutStats{
		Dropped:   stdoutDropped.Load(),
		Truncated: stdoutTruncated.Load(),
	}
}
