package anomaly

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/decembaek/socquery-agent-go/internal/scanner"
)

// EngineConfig 는 이상치 분석 엔진 설정입니다.
type EngineConfig struct {
	Resource ResourceThresholds
	Process  ProcessConfig
	Network  NetworkConfig
	USB      USBConfig
}

// DefaultEngineConfig 는 기본 엔진 설정을 반환합니다.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		Resource: DefaultResourceThresholds(),
		Process:  DefaultProcessConfig(),
		Network:  DefaultNetworkConfig(),
		USB:      DefaultUSBConfig(),
	}
}

// AlertHandler 는 이상치 알림을 처리하는 콜백 함수입니다.
type AlertHandler func(alert Alert)

// Engine 는 4축 이상치 분석 엔진입니다.
// 스캐너 결과를 받아 각 축의 Detector에 피딩하고,
// 이상 발생 시 AlertHandler를 통해 통보합니다.
type Engine struct {
	mu sync.RWMutex

	resource *ResourceDetector
	process  *ProcessDetector
	network  *NetworkDetector
	usb      *USBDetector

	handlers []AlertHandler

	// 알림 이력 (최근 100건)
	alertHistory []Alert
	maxHistory   int
}

// NewEngine 는 새 이상치 분석 엔진을 생성합니다.
func NewEngine(config EngineConfig) *Engine {
	return &Engine{
		resource:     NewResourceDetector(config.Resource),
		process:      NewProcessDetector(config.Process),
		network:      NewNetworkDetector(config.Network),
		usb:          NewUSBDetector(config.USB),
		alertHistory: make([]Alert, 0),
		maxHistory:   100,
	}
}

// OnAlert 는 이상치 알림 핸들러를 등록합니다.
// 여러 핸들러를 등록할 수 있습니다 (MQTT 발행, 로깅 등).
func (e *Engine) OnAlert(handler AlertHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers = append(e.handlers, handler)
}

// NetworkDetector 는 외부에서 MQTT 끊김 통보를 위해 NetworkDetector를 반환합니다.
func (e *Engine) NetworkDetector() *NetworkDetector {
	return e.network
}

// FeedHealth 는 Health 스캔 결과를 엔진에 공급하고 리소스 이상치를 검사합니다.
func (e *Engine) FeedHealth(info *scanner.HealthInfo) {
	e.resource.Feed(info)
	e.processAlerts(e.resource.Check())
}

// FeedProcesses 는 프로세스 스캔 결과를 엔진에 공급하고 프로세스 이상치를 검사합니다.
func (e *Engine) FeedProcesses(result *scanner.ProcessScanResult) {
	e.process.Feed(result)
	e.processAlerts(e.process.Check())
}

// FeedNetwork 는 네트워크 스캔 결과를 엔진에 공급하고 네트워크 이상치를 검사합니다.
func (e *Engine) FeedNetwork(result *scanner.NetworkScanResult) {
	e.network.Feed(result)
	e.processAlerts(e.network.Check())
}

// FeedUSB 는 USB 스캔 결과를 엔진에 공급하고 USB 변경을 검사합니다.
func (e *Engine) FeedUSB(devices []scanner.USBDevice) {
	e.usb.Feed(devices)
	e.processAlerts(e.usb.Check())
}

// CheckAll 은 모든 축의 이상치를 한 번에 검사합니다. (주기적 호출용)
func (e *Engine) CheckAll() []Alert {
	var all []Alert
	all = append(all, e.resource.Check()...)
	all = append(all, e.process.Check()...)
	all = append(all, e.network.Check()...)
	// USB는 Feed 시점에 체크하므로 여기서는 생략
	return all
}

// GetAlertHistory 는 최근 알림 이력을 반환합니다.
func (e *Engine) GetAlertHistory() []Alert {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]Alert, len(e.alertHistory))
	copy(result, e.alertHistory)
	return result
}

// processAlerts 는 감지된 알림들을 처리합니다.
func (e *Engine) processAlerts(alerts []Alert) {
	if len(alerts) == 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, alert := range alerts {
		// 이력 추가
		e.alertHistory = append(e.alertHistory, alert)
		if len(e.alertHistory) > e.maxHistory {
			e.alertHistory = e.alertHistory[len(e.alertHistory)-e.maxHistory:]
		}

		// 콘솔 로깅
		logAlert(alert)

		// 핸들러 호출
		for _, handler := range e.handlers {
			handler(alert)
		}
	}
}

// logAlert 는 알림을 콘솔에 출력합니다.
func logAlert(a Alert) {
	icon := "ℹ"
	switch a.Severity {
	case SeverityWarning:
		icon = "⚠"
	case SeverityCritical:
		icon = "🚨"
	}
	log.Printf("[Anomaly] %s [%s/%s] %s\n", icon, a.Category, a.Severity, a.Message)
}

// PrintStatus 는 현재 엔진 상태 요약을 콘솔에 출력합니다. (테스트/디버그용)
func (e *Engine) PrintStatus() {
	e.mu.RLock()
	defer e.mu.RUnlock()

	fmt.Println("=== Anomaly Engine 상태 ===")
	fmt.Printf("등록된 핸들러: %d개\n", len(e.handlers))
	fmt.Printf("알림 이력: %d건\n", len(e.alertHistory))

	if len(e.alertHistory) > 0 {
		fmt.Println("\n[최근 알림]")
		start := 0
		if len(e.alertHistory) > 10 {
			start = len(e.alertHistory) - 10
		}
		for _, a := range e.alertHistory[start:] {
			fmt.Printf("  %s [%s/%s] %s\n",
				a.Timestamp.Format("15:04:05"), a.Category, a.Severity, a.Message)
		}
	}
}

// AlertToJSON 는 Alert를 JSON 바이트로 변환합니다. (MQTT 발행용)
func AlertToJSON(a Alert) ([]byte, error) {
	return json.Marshal(a)
}

// FormatAlertSummary 는 테스트용 알림 요약 문자열을 반환합니다.
func FormatAlertSummary(alerts []Alert) string {
	if len(alerts) == 0 {
		return "이상 없음"
	}
	result := fmt.Sprintf("%d건의 이상 감지:\n", len(alerts))
	for i, a := range alerts {
		icon := "ℹ"
		switch a.Severity {
		case SeverityWarning:
			icon = "⚠"
		case SeverityCritical:
			icon = "🚨"
		}
		result += fmt.Sprintf("  %d) %s [%s] %s\n", i+1, icon, a.Category, a.Message)
	}
	return result
}
