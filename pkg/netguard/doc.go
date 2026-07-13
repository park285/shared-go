// Package netguard는 외부 HTTP 대상과 dial 주소를 fail-closed로 검증합니다.
// IsBlockedAddr는 private, loopback, link-local, CGNAT, benchmark, documentation, NAT64 등
// public egress 대상으로 사용할 수 없는 special-purpose 대역을 기본 차단합니다. private overlay가
// 필요하면 Policy.AllowedIPPrefixes에 필요한 prefix만 명시하고, Policy.AllowedHosts 및 최초 요청
// ValidateURL 검증을 함께 사용하십시오. dial 경로는 IP와 port 정책을 강제합니다.
package netguard
