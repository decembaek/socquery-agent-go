// Package executor 는 서버에서 MQTT로 수신된 명령을 실행하는 실행기입니다.
//
// 플로우: SaaS 서버 → MQTT command 토픽 → Agent executor → 실행 → MQTT response 토픽
package executor

import "time"

// Command 는 서버에서 수신된 명령 구조체입니다.
type Command struct {
	CommandID string                 `json:"commandId"` // 명령 고유 ID (응답 추적용)
	Action    string                 `json:"action"`    // 명령 타입
	Params    map[string]interface{} `json:"params"`    // 명령별 파라미터
	Timeout   int                    `json:"timeout"`   // 실행 제한 시간 (초, 기본 30)
}

// Response 는 명령 실행 결과 구조체입니다.
type Response struct {
	CommandID  string      `json:"commandId"`  // 요청의 commandId와 동일
	Action     string      `json:"action"`     // 실행한 action
	ExitCode   int         `json:"exitCode"`   // 0 = 성공, 그 외 = 실패
	Data       interface{} `json:"data"`       // 실행 결과 데이터
	Error      string      `json:"error,omitempty"` // 에러 메시지 (실패 시)
	ExecutedAt int64       `json:"executedAt"` // 실행 시작 시각 (unix)
	DurationMs int64       `json:"durationMs"` // 실행 소요 시간 (ms)
}

// 지원하는 Action 목록
const (
	ActionScanUSB     = "scan_usb"
	ActionScanProcess = "scan_process"
	ActionScanNetwork = "scan_network"
	ActionScanHealth  = "scan_health"
	ActionScanAll     = "scan_all"
	ActionHTTPRequest = "http_request" // HTTP 프록시 요청
	ActionPing        = "ping"         // 연결 확인
)

// GetTimeout 은 명령의 타임아웃을 Duration으로 반환합니다.
func (c *Command) GetTimeout() time.Duration {
	if c.Timeout > 0 {
		return time.Duration(c.Timeout) * time.Second
	}
	return 30 * time.Second // 기본 30초
}
