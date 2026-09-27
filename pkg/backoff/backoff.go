package backoff

import (
	"math"
	"math/rand/v2"
	"time"
)

// NextExponentialBackoff는 현재 backoff 값을 두 배로 늘리고 step과 maxInterval 경계를 적용합니다.
func NextExponentialBackoff(current, maxInterval, step time.Duration) time.Duration {
	if current <= 0 {
		return capInterval(step, maxInterval)
	}

	return capInterval(max(doubleWithCap(current, maxInterval), step), maxInterval)
}

// ComputeExponentialBackoff는 attempt 번호를 기반으로 base*2^attempt 지연값을 계산합니다.
func ComputeExponentialBackoff(attempt int, base, maxInterval, jitter time.Duration) time.Duration {
	if invalidAttempt(attempt, base) {
		return 0
	}

	candidate := computeCandidate(attempt, base, maxInterval)

	return addJitter(candidate, jitter)
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

	if current > math.MaxInt64/2 {
		return time.Duration(math.MaxInt64)
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

	// #nosec G404 -- 재시도 분산용 jitter이며 보안 목적의 난수가 아니다.
	sum := candidate + time.Duration(rand.Int64N(int64(jitter)))
	if sum < candidate {
		return time.Duration(math.MaxInt64)
	}

	return sum
}
