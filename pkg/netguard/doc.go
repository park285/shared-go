// Package netguard는 외부 HTTP 대상과 dial 주소를 fail-closed로 검증합니다.
//
// IsBlockedAddr는 표준 private/loopback/link-local/ULA 대역을 막지만 CGNAT(100.64.0.0/10), benchmarking, NAT64, documentation 대역은 막지 않습니다.
// 강한 경계가 필요하면 Policy.AllowedHosts allowlist와 최초 요청 ValidateURL을 함께 쓰십시오. dial 경로는 IP+port만 강제합니다.
package netguard
