package pgxdb

import (
	"errors"
	"net"
	"strings"
)

func ShouldFallbackToLocalhost(err error, host string) bool {
	if err == nil {
		return false
	}
	if !isFallbackEligibleHost(host) {
		return false
	}

	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		return strings.EqualFold(dnsErr.Name, host)
	}

	lower := strings.ToLower(err.Error())
	hostLower := strings.ToLower(host)
	if strings.Contains(lower, "lookup "+hostLower) && strings.Contains(lower, "no such host") {
		return true
	}
	return strings.Contains(lower, "no such host") && strings.Contains(lower, hostLower)
}

func isFallbackEligibleHost(host string) bool {
	if host == "" || host == "127.0.0.1" || strings.EqualFold(host, "localhost") {
		return false
	}
	return strings.EqualFold(host, "postgres")
}
