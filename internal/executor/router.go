package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/decembaek/socquery-agent-go/internal/scanner"
)

// Router 는 MQTT로 수신된 명령을 적절한 핸들러로 라우팅하는 실행기입니다.
type Router struct {
	// ResponseHandler 는 명령 실행 결과를 발행하는 콜백입니다.
	// (호출 측에서 MQTT 발행 로직을 주입)
	ResponseHandler func(response Response)
}

// NewRouter 는 새 Router를 생성합니다.
func NewRouter() *Router {
	return &Router{}
}

// OnResponse 는 명령 결과 핸들러를 등록합니다.
func (r *Router) OnResponse(handler func(Response)) {
	r.ResponseHandler = handler
}

// HandleCommand 는 수신된 명령을 파싱하고 실행합니다.
// MQTT 메시지 payload (JSON bytes)를 받아 비동기 실행합니다.
func (r *Router) HandleCommand(payload []byte) {
	// 1. 명령 파싱
	var cmd Command
	if err := json.Unmarshal(payload, &cmd); err != nil {
		log.Printf("[Executor] 명령 파싱 실패: %v | raw: %s\n", err, string(payload))
		return
	}

	if cmd.CommandID == "" {
		log.Printf("[Executor] commandId 누락: %s\n", string(payload))
		return
	}

	// 2. 비동기 실행 (타임아웃 적용)
	go r.execute(cmd)
}

// execute 는 실제 명령 실행을 수행합니다.
func (r *Router) execute(cmd Command) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), cmd.GetTimeout())
	defer cancel()

	resp := Response{
		CommandID:  cmd.CommandID,
		Action:     cmd.Action,
		ExecutedAt: start.Unix(),
	}

	// Action별 라우팅
	switch cmd.Action {
	case ActionPing:
		resp.Data = map[string]interface{}{
			"pong":      true,
			"timestamp": time.Now().Unix(),
		}
		resp.ExitCode = 0

	case ActionHTTPRequest:
		r.handleHTTPRequest(ctx, cmd, &resp)

	case ActionScanUSB:
		r.handleScanUSB(&resp)

	case ActionScanHealth:
		r.handleScanHealth(&resp)

	case ActionScanProcess:
		r.handleScanProcess(&resp)

	case ActionScanNetwork:
		r.handleScanNetwork(&resp)

	case ActionScanAll:
		r.handleScanAll(&resp)

	default:
		resp.ExitCode = 1
		resp.Error = fmt.Sprintf("알 수 없는 action: %s", cmd.Action)
	}

	resp.DurationMs = time.Since(start).Milliseconds()

	// 결과 발행
	if r.ResponseHandler != nil {
		r.ResponseHandler(resp)
	}
}

// ---------------------------------------------------------------------------
// Action 핸들러들
// ---------------------------------------------------------------------------

func (r *Router) handleHTTPRequest(ctx context.Context, cmd Command, resp *Response) {
	// params에서 HTTP 요청 파싱
	httpReq, err := ParseHTTPRequest(cmd.Params)
	if err != nil {
		resp.ExitCode = 1
		resp.Error = fmt.Sprintf("HTTP 요청 파싱 실패: %v", err)
		return
	}

	// HTTP 요청 실행
	httpResp, err := ExecuteHTTPRequest(ctx, httpReq)
	if err != nil {
		resp.ExitCode = 1
		resp.Error = fmt.Sprintf("HTTP 요청 실패: %v", err)
		return
	}

	resp.ExitCode = 0
	resp.Data = httpResp
}

func (r *Router) handleScanUSB(resp *Response) {
	devices, err := scanner.ScanUSBDevices()
	if err != nil {
		resp.ExitCode = 1
		resp.Error = fmt.Sprintf("USB 스캔 실패: %v", err)
		return
	}
	resp.ExitCode = 0
	resp.Data = map[string]interface{}{
		"deviceCount": len(devices),
		"devices":     devices,
	}
}

func (r *Router) handleScanHealth(resp *Response) {
	info, err := scanner.ScanHealth()
	if err != nil {
		resp.ExitCode = 1
		resp.Error = fmt.Sprintf("Health 스캔 실패: %v", err)
		return
	}
	resp.ExitCode = 0
	resp.Data = map[string]interface{}{
		"cpu":    info.CPU,
		"memory": info.Memory,
		"disk":   info.Disk,
		"os":     info.OS,
	}
}

func (r *Router) handleScanProcess(resp *Response) {
	result, err := scanner.ScanProcesses()
	if err != nil {
		resp.ExitCode = 1
		resp.Error = fmt.Sprintf("Process 스캔 실패: %v", err)
		return
	}
	resp.ExitCode = 0
	resp.Data = map[string]interface{}{
		"processCount": result.TotalCount,
		"topCpu":       result.TopCPU,
		"topMem":       result.TopMem,
	}
}

func (r *Router) handleScanNetwork(resp *Response) {
	result, err := scanner.ScanNetwork()
	if err != nil {
		resp.ExitCode = 1
		resp.Error = fmt.Sprintf("Network 스캔 실패: %v", err)
		return
	}
	resp.ExitCode = 0
	resp.Data = map[string]interface{}{
		"interfaces":  result.Interfaces,
		"connections": result.Connections,
		"dns":         result.DNS,
	}
}

func (r *Router) handleScanAll(resp *Response) {
	result := make(map[string]interface{})

	// USB
	if devices, err := scanner.ScanUSBDevices(); err == nil {
		result["usb"] = map[string]interface{}{
			"deviceCount": len(devices),
			"devices":     devices,
		}
	} else {
		result["usb"] = map[string]interface{}{"error": err.Error()}
	}

	// Health
	if info, err := scanner.ScanHealth(); err == nil {
		result["health"] = map[string]interface{}{
			"cpu":    info.CPU,
			"memory": info.Memory,
			"disk":   info.Disk,
			"os":     info.OS,
		}
	} else {
		result["health"] = map[string]interface{}{"error": err.Error()}
	}

	// Process
	if procs, err := scanner.ScanProcesses(); err == nil {
		result["process"] = map[string]interface{}{
			"processCount": procs.TotalCount,
			"topCpu":       procs.TopCPU,
			"topMem":       procs.TopMem,
		}
	} else {
		result["process"] = map[string]interface{}{"error": err.Error()}
	}

	// Network
	if net, err := scanner.ScanNetwork(); err == nil {
		result["network"] = map[string]interface{}{
			"interfaces":  net.Interfaces,
			"connections": net.Connections,
			"dns":         net.DNS,
		}
	} else {
		result["network"] = map[string]interface{}{"error": err.Error()}
	}

	resp.ExitCode = 0
	resp.Data = result
}
