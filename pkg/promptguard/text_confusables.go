package promptguard

import "strings"

// confusables.Skeleton은 라틴 m/M을 "rn"으로 접는다. 그대로 두면 m을 포함한 모든
// 룰 패턴·phrase(prompt, system, message, developermode 등)가 norm/joined view와
// 영원히 불일치하므로, skeleton 직전에 m/M을 사적영역 문자로 빼돌려 보존한 뒤
// 키릴 м이 skeleton에서 남기는 ʍ까지 라틴 m으로 마저 접는다.
const (
	latinMLowerPlaceholder = "\uE000"
	latinMUpperPlaceholder = "\uE001"
)

var (
	skeletonMGuard = strings.NewReplacer(
		"m", latinMLowerPlaceholder,
		"M", latinMUpperPlaceholder,
	)
	skeletonMRestore = strings.NewReplacer(
		latinMLowerPlaceholder, "m",
		latinMUpperPlaceholder, "M",
		"ʍ", "m",
	)
)
