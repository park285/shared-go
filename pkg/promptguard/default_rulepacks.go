package promptguard

import "embed"

const defaultRulepacksRoot = "rulepacks"

//go:embed rulepacks/*.yml
var defaultRulepackFS embed.FS
