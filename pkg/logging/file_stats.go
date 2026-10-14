package logging

import "sync/atomic"

var fileWriteFailed atomic.Uint64

// FileStats는 파일 lane 기록 결과의 프로세스 누적 counter다.
// WriteFailed는 동기 파일 lane 쓰기가 오류 또는 short write로 끝난 건수이며, 종료 시 파일
// lane에 남기는 stdout 유실 요약 기록의 실패도 포함한다. 실패한 record는 일부만 기록됐을 수
// 있다. 파일 lane이 없는 console-only logger는 집계하지 않는다.
type FileStats struct {
	WriteFailed uint64
}

// SnapshotFile은 재설정하지 않는 단조 증가 프로세스 counter를 반환한다.
// 0은 알려진 실패가 없다는 뜻일 뿐 내구 저장 성공을 증명하지 않는다.
func SnapshotFile() FileStats {
	return FileStats{WriteFailed: fileWriteFailed.Load()}
}
