package scanner

import (
	"net"
)

// NetworkInterface 는 네트워크 인터페이스 정보를 나타냅니다.
type NetworkInterface struct {
	Name    string   `json:"name"`              // 인터페이스 이름 (eth0, en0 등)
	MAC     string   `json:"mac"`               // MAC 주소
	IPv4    []string `json:"ipv4"`              // IPv4 주소 목록
	IPv6    []string `json:"ipv6,omitempty"`    // IPv6 주소 목록
	Status  string   `json:"status"`            // up / down
	MTU     int      `json:"mtu"`               // MTU
	Flags   string   `json:"flags"`             // 인터페이스 플래그
	RxBytes uint64   `json:"rxBytes,omitempty"` // 수신 바이트 (Linux만)
	TxBytes uint64   `json:"txBytes,omitempty"` // 송신 바이트 (Linux만)
}

// ConnectionStats 는 네트워크 연결 통계를 나타냅니다.
type ConnectionStats struct {
	Established int `json:"established"` // ESTABLISHED 연결 수
	Listening   int `json:"listening"`   // LISTEN 연결 수
	TimeWait    int `json:"timeWait"`    // TIME_WAIT 연결 수
	CloseWait   int `json:"closeWait"`   // CLOSE_WAIT 연결 수
	Total       int `json:"total"`       // 전체 연결 수
}

// DNSCheck 는 DNS 확인 결과를 나타냅니다.
type DNSCheck struct {
	Server    string `json:"server"`    // DNS 서버 주소
	Reachable bool   `json:"reachable"` // DNS 서버 접근 가능 여부
	LatencyMs int64  `json:"latencyMs"` // DNS 응답 시간 (ms)
}

// NetworkScanResult 는 네트워크 스캔 결과를 나타냅니다.
type NetworkScanResult struct {
	Interfaces  []NetworkInterface `json:"interfaces"`  // 네트워크 인터페이스 목록
	Connections ConnectionStats    `json:"connections"` // 연결 통계
	DNS         DNSCheck           `json:"dns"`         // DNS 상태
	ExternalIP  string             `json:"externalIp"`  // 외부 IP (선택)
}

// ScanNetwork 는 현재 시스템의 네트워크 상태를 스캔합니다.
func ScanNetwork() (*NetworkScanResult, error) {
	result := &NetworkScanResult{}

	// 1. 네트워크 인터페이스 (Go net 표준 라이브러리 - 크로스플랫폼)
	result.Interfaces = getNetworkInterfaces()

	// 2. 연결 통계 + DNS (OS별 구현)
	connStats, dns := scanNetworkPlatform()
	result.Connections = connStats
	result.DNS = dns

	return result, nil
}

// getNetworkInterfaces 는 Go 표준 라이브러리로 인터페이스 목록을 가져옵니다.
func getNetworkInterfaces() []NetworkInterface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var result []NetworkInterface
	for _, iface := range ifaces {
		// 루프백, 가상 인터페이스 등 제외하지 않고 모두 포함
		ni := NetworkInterface{
			Name:  iface.Name,
			MAC:   iface.HardwareAddr.String(),
			MTU:   iface.MTU,
			Flags: iface.Flags.String(),
		}

		if iface.Flags&net.FlagUp != 0 {
			ni.Status = "up"
		} else {
			ni.Status = "down"
		}

		// IP 주소 수집
		addrs, err := iface.Addrs()
		if err == nil {
			for _, addr := range addrs {
				ip := addr.String()
				if isIPv4(ip) {
					ni.IPv4 = append(ni.IPv4, ip)
				} else {
					ni.IPv6 = append(ni.IPv6, ip)
				}
			}
		}

		// MAC 없는 루프백 등은 있어도 유용, 그대로 포함
		// 단 IP 주소가 하나도 없고 down인 것은 제외
		if ni.Status == "up" || len(ni.IPv4) > 0 || len(ni.IPv6) > 0 {
			result = append(result, ni)
		}
	}

	return result
}

// isIPv4 는 주소 문자열이 IPv4인지 확인합니다.
func isIPv4(addr string) bool {
	// CIDR 형태 "192.168.1.1/24" 에서 IP 부분만 추출
	ip, _, err := net.ParseCIDR(addr)
	if err != nil {
		ip = net.ParseIP(addr)
	}
	if ip == nil {
		return false
	}
	return ip.To4() != nil
}
