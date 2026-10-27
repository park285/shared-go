package irisdurable

// RetryBackoffMaxShift는 inbox 재시도 지수 백오프에서 지수(2^n)의 상한이다.
//
// Iris admission 보존·자동 replay 지평은 이 패키지가 사본으로 들고 있지 않는다. 정본은 Iris 계약 파일과
// iris-client-go 공개 상수이며, 소비자가 그 값을 pgstore.Options와 contracttest.RetentionFixture에 넘긴다.
const RetryBackoffMaxShift = 8
