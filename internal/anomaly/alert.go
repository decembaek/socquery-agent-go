// Package anomaly 는 4축 이상치 분석 엔진을 제공합니다.
//
// 4축: 리소스(CPU/Mem/Disk) + 앱 생존(프로세스) + 통신(Network/MQTT) + 주변장치(USB)
package anomaly

import "time"

// Severity 는 이상치 심각도 레벨입니다.
type Severity string

const (
	SeverityInfo     Severity = "info"     // 정보성 (구성 변경 등)
	SeverityWarning  Severity = "warning"  // 경고 (임계치 초과, 주의 필요)
	SeverityCritical Severity = "critical" // 심각 (장애, 즉시 대응 필요)
)

// Category 는 이상치가 발생한 축(카테고리)입니다.
type Category string

const (
	CategoryResource Category = "resource" // 1축: CPU/메모리/디스크
	CategoryProcess  Category = "process"  // 2축: 앱 프로세스
	CategoryNetwork  Category = "network"  // 3축: 네트워크/MQTT
	CategoryUSB      Category = "usb"      // 4축: USB 디바이스
)

// Alert 는 이상치 감지 이벤트입니다.
type Alert struct {
	ID        string                 `json:"id"`        // 고유 ID (카테고리-타입-타임스탬프)
	Category  Category               `json:"category"`  // 이상치 축
	Severity  Severity               `json:"severity"`  // 심각도
	Type      string                 `json:"type"`      // 구체적 이상 유형 (cpu_spike, process_missing 등)
	Message   string                 `json:"message"`   // 사람이 읽을 수 있는 메시지
	Details   map[string]interface{} `json:"details"`   // 상세 데이터
	Timestamp time.Time              `json:"timestamp"` // 발생 시각
}

// Detector 는 이상치 감지기의 공통 인터페이스입니다.
type Detector interface {
	// Name 은 감지기 이름을 반환합니다.
	Name() string
	// Check 는 현재 상태를 평가하고, 이상 발생 시 Alert를 반환합니다.
	// 정상이면 nil을 반환합니다.
	Check() []Alert
}
