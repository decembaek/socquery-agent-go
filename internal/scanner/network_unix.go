//go:build darwin || linux

package scanner

import (
	"net"
	"os/exec"
	"strings"
	"time"
)

// scanNetworkPlatform 는 Unix 계열에서 연결 통계와 DNS 정보를 수집합니다.
func scanNetworkPlatform() (ConnectionStats, DNSCheck) {
	return getConnectionStatsUnix(), checkDNS()
}

// getConnectionStatsUnix 는 netstat으로 연결 통계를 가져옵니다.
func getConnectionStatsUnix() ConnectionStats {
	var stats ConnectionStats

	cmd := exec.Command("netstat", "-an")
	output, err := cmd.Output()
	if err != nil {
		return stats
	}

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "tcp") {
			continue
		}

		stats.Total++

		upper := strings.ToUpper(line)
		switch {
		case strings.Contains(upper, "ESTABLISHED"):
			stats.Established++
		case strings.Contains(upper, "LISTEN"):
			stats.Listening++
		case strings.Contains(upper, "TIME_WAIT"):
			stats.TimeWait++
		case strings.Contains(upper, "CLOSE_WAIT"):
			stats.CloseWait++
		}
	}

	return stats
}

// checkDNS 는 DNS 서버 연결을 확인합니다.
func checkDNS() DNSCheck {
	dns := DNSCheck{
		Server: "8.8.8.8",
	}

	start := time.Now()
	_, err := net.LookupHost("dns.google")
	elapsed := time.Since(start)

	if err == nil {
		dns.Reachable = true
		dns.LatencyMs = elapsed.Milliseconds()
	}

	return dns
}
