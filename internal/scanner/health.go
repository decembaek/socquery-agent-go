package scanner

// HealthInfo 는 OS 헬스 메트릭 정보를 나타냅니다.
type HealthInfo struct {
	CPU    CPUInfo    `json:"cpu"`
	Memory MemoryInfo `json:"memory"`
	Disk   DiskInfo   `json:"disk"`
	OS     OSInfo     `json:"os"`
}

// CPUInfo 는 CPU 사용률 정보를 나타냅니다.
type CPUInfo struct {
	UsagePercent float64   `json:"usagePercent"` // CPU 사용률 (%)
	Cores        int       `json:"cores"`        // CPU 코어 수
	LoadAvg      []float64 `json:"loadAvg"`      // 로드 평균 [1m, 5m, 15m]
}

// MemoryInfo 는 메모리 사용 정보를 나타냅니다.
type MemoryInfo struct {
	TotalMB      uint64  `json:"totalMB"`      // 전체 메모리 (MB)
	UsedMB       uint64  `json:"usedMB"`       // 사용 중 메모리 (MB)
	AvailableMB  uint64  `json:"availableMB"`  // 사용 가능 메모리 (MB)
	UsagePercent float64 `json:"usagePercent"` // 메모리 사용률 (%)
}

// DiskInfo 는 디스크 사용 정보를 나타냅니다.
type DiskInfo struct {
	TotalGB      float64 `json:"totalGB"`      // 전체 디스크 (GB)
	UsedGB       float64 `json:"usedGB"`       // 사용 중 (GB)
	FreeGB       float64 `json:"freeGB"`       // 여유 공간 (GB)
	UsagePercent float64 `json:"usagePercent"` // 디스크 사용률 (%)
}

// OSInfo 는 운영체제 정보를 나타냅니다.
type OSInfo struct {
	Platform      string `json:"platform"`      // OS 플랫폼 (linux, windows, darwin)
	Hostname      string `json:"hostname"`      // 호스트명
	KernelVersion string `json:"kernelVersion"` // 커널 버전
	Uptime        uint64 `json:"uptime"`        // 시스템 업타임 (초)
}

// ScanHealth 는 현재 시스템의 OS 헬스 메트릭을 수집합니다.
// OS별로 다른 구현이 빌드 태그를 통해 선택됩니다.
func ScanHealth() (*HealthInfo, error) {
	return scanHealth()
}
