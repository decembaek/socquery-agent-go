package scanner

// ProcessInfo 는 프로세스 정보를 나타냅니다.
type ProcessInfo struct {
	PID       int     `json:"pid"`       // 프로세스 ID
	Name      string  `json:"name"`      // 프로세스 이름
	User      string  `json:"user"`      // 실행 사용자
	CPUPct    float64 `json:"cpu"`       // CPU 사용률 (%)
	MemPct    float64 `json:"mem"`       // 메모리 사용률 (%)
	MemMB     float64 `json:"memMB"`     // 메모리 사용량 (MB)
	Status    string  `json:"status"`    // 상태 (running, sleeping, etc.)
	StartedAt string  `json:"startedAt"` // 시작 시간
}

// ProcessScanResult 는 프로세스 스캔 결과를 나타냅니다.
type ProcessScanResult struct {
	TotalCount int           `json:"processCount"` // 전체 프로세스 수
	TopCPU     []ProcessInfo `json:"topCpu"`       // CPU 사용량 상위 프로세스
	TopMem     []ProcessInfo `json:"topMem"`       // 메모리 사용량 상위 프로세스
}

// ScanProcesses 는 현재 실행 중인 프로세스를 스캔합니다.
// CPU/메모리 기준 상위 N개를 반환합니다.
func ScanProcesses() (*ProcessScanResult, error) {
	return scanProcesses()
}
