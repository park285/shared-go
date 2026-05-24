package backoff

import (
	"math/rand"
	"time"
)

// NextExponentialBackoff는 현재 backoff 값을 두 배로 늘리고 step과 maxInterval 경계를 적용합니다.
func NextExponentialBackoff(current, maxInterval, step time.Duration) time.Duration {
	if current <= 0 {
		return step
	}

	next := min(max(current*2, step), maxInterval)
	return next
}

// ComputeExponentialBackoff는 attempt 번호를 기반으로 base*2^attempt 지연값을 계산합니다.
func ComputeExponentialBackoff(attempt int, base, maxInterval, jitter time.Duration) time.Duration {
	if invalidAttempt(attempt, base) {
		return 0
	}

	candidate := computeCandidate(attempt, base, maxInterval)
	return addJitter(candidate, jitter)
}

// ComputeExponentialBackoffHalfJitter는 base*2^attempt를 maxInterval로 cap한 값(cap)에 대해
// [cap/2, cap) 범위의 backoff 값을 반환합니다. cap이 1보다 작거나 같은 극단적 케이스에서는
// jitter가 의미 없어 cap 그대로 반환합니다.
// 분포는 cbgk legacy retryDelay/consumerRetryDelay와 통계적으로 동등합니다 (홀수 cap에서는 본 함수가
// 상한 1 unit을 추가로 포함해 약간 더 균등하지만 production duration 규모에서는 ±5% 이내).
func ComputeExponentialBackoffHalfJitter(attempt int, base, maxInterval time.Duration) time.Duration {
	if invalidAttempt(attempt, base) || maxInterval <= 0 {
		return 0
	}

	cap := capInterval(computeCandidate(attempt, base, maxInterval), maxInterval)
	if cap <= 0 {
		return 0
	}

	half := cap / 2
	upper := cap - half
	if upper <= 0 {
		return cap
	}
	result := half + time.Duration(rand.Int63n(int64(upper)))
	if result <= 0 {
		return cap
	}
	return result
}

func invalidAttempt(attempt int, base time.Duration) bool {
	return attempt < 0 || base <= 0
}

func computeCandidate(attempt int, base, maxInterval time.Duration) time.Duration {
	candidate := base
	for range attempt {
		candidate = doubleWithCap(candidate, maxInterval)
	}
	return capInterval(candidate, maxInterval)
}

func doubleWithCap(current, maxInterval time.Duration) time.Duration {
	if maxInterval > 0 && current > maxInterval/2 {
		return maxInterval
	}
	return current * 2
}

func capInterval(candidate, maxInterval time.Duration) time.Duration {
	if maxInterval > 0 && candidate > maxInterval {
		return maxInterval
	}
	return candidate
}

func addJitter(candidate, jitter time.Duration) time.Duration {
	if jitter <= 0 {
		return candidate
	}
	return candidate + time.Duration(rand.Int63n(int64(jitter)))
}
