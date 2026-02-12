package anomaly

import (
	"fmt"
	"time"

	"github.com/decembaek/socquery-agent-go/internal/scanner"
)

// WatchProcess 는 감시할 프로세스 정의입니다.
type WatchProcess struct {
	Name     string // 프로세스 이름 (예: "nginx", "node")
	Critical bool   // true면 없을 때 critical, false면 warning
}

// ProcessConfig 는 프로세스 감지기 설정입니다.
type ProcessConfig struct {
	// WatchList 는 존재 여부를 감시할 프로세스 목록입니다.
	// 비어 있으면 존재 여부 감시를 하지 않습니다.
	WatchList []WatchProcess

	// CPU/MEM 이상 감지 설정
	CPUHogThresholdPct float64 // 이 값 이상이면 CPU 호그 의심 (%, 기본 80)
	MemHogThresholdPct float64 // 이 값 이상이면 MEM 호그 의심 (%, 기본 50)
	HogDurationCount   int     // 연속 N회 상위에 있으면 알림 (기본 5)
}

// DefaultProcessConfig 는 기본 프로세스 감지 설정입니다.
func DefaultProcessConfig() ProcessConfig {
	return ProcessConfig{
		WatchList:          nil, // 사용자가 설정
		CPUHogThresholdPct: 80,
		MemHogThresholdPct: 50,
		HogDurationCount:   5,
	}
}

// processHogState 는 프로세스별 호그 상태를 추적합니다.
type processHogState struct {
	consecutiveCPU int       // CPU 호그 연속 횟수
	consecutiveMem int       // MEM 호그 연속 횟수
	firstSeenAt    time.Time // 처음 감지 시각
	alertedCPU     bool
	alertedMem     bool
	lastCPUAlertAt time.Time // CPU 호그 마지막 알림 시각
	lastMemAlertAt time.Time // MEM 호그 마지막 알림 시각
}

// ProcessDetector 는 프로세스 존재 여부 및 리소스 호그를 감지합니다.
type ProcessDetector struct {
	config    ProcessConfig
	hogStates map[string]*processHogState // key: process name
	// 존재 여부 추적: 지난 체크에서 있었는데 지금 없으면 = 사라짐
	lastPresent map[string]bool // key: process name (watchlist)
	lastResult  *scanner.ProcessScanResult
}

// NewProcessDetector 는 새 프로세스 감지기를 생성합니다.
func NewProcessDetector(config ProcessConfig) *ProcessDetector {
	return &ProcessDetector{
		config:      config,
		hogStates:   make(map[string]*processHogState),
		lastPresent: make(map[string]bool),
	}
}

func (d *ProcessDetector) Name() string {
	return "process"
}

// Feed 는 스캐너에서 받은 프로세스 결과를 감지기에 공급합니다.
func (d *ProcessDetector) Feed(result *scanner.ProcessScanResult) {
	d.lastResult = result
}

// Check 는 프로세스 이상치를 평가합니다.
func (d *ProcessDetector) Check() []Alert {
	if d.lastResult == nil {
		return nil
	}

	var alerts []Alert
	now := time.Now()

	// 1. 감시 프로세스 존재 여부 확인
	alerts = append(alerts, d.checkWatchList(now)...)

	// 2. CPU/MEM 호그 감지
	alerts = append(alerts, d.checkHogs(now)...)

	return alerts
}

// checkWatchList 는 감시 프로세스의 존재 여부를 확인합니다.
func (d *ProcessDetector) checkWatchList(now time.Time) []Alert {
	if len(d.config.WatchList) == 0 {
		return nil
	}

	var alerts []Alert

	// 현재 실행 중인 프로세스 이름 집합 구성
	running := make(map[string]bool)
	allProcs := append(d.lastResult.TopCPU, d.lastResult.TopMem...)
	for _, p := range allProcs {
		running[p.Name] = true
	}

	// 별도로 전체 프로세스를 스캔해야 하나, TopCPU/TopMem에 없을 수 있으므로
	// 여기서는 최선의 근사치. 정확하려면 전체 프로세스 목록이 필요.
	// TODO: scanner에서 전체 프로세스 존재 여부 API 추가 시 개선

	for _, wp := range d.config.WatchList {
		present := running[wp.Name]
		wasPresentBefore := d.lastPresent[wp.Name]

		if !present && wasPresentBefore {
			// 프로세스가 사라짐!
			severity := SeverityWarning
			if wp.Critical {
				severity = SeverityCritical
			}

			alerts = append(alerts, Alert{
				ID:       fmt.Sprintf("process-missing-%s-%d", wp.Name, now.Unix()),
				Category: CategoryProcess,
				Severity: severity,
				Type:     "process_missing",
				Message:  fmt.Sprintf("감시 프로세스 '%s' 가 실행 중이지 않습니다", wp.Name),
				Details: map[string]interface{}{
					"processName": wp.Name,
					"critical":    wp.Critical,
				},
				Timestamp: now,
			})
		} else if present && !wasPresentBefore && wasPresentBefore {
			// 복구됨 (이전에 없었는데 다시 나타남 - 최초 실행은 제외)
			alerts = append(alerts, Alert{
				ID:       fmt.Sprintf("process-recovered-%s-%d", wp.Name, now.Unix()),
				Category: CategoryProcess,
				Severity: SeverityInfo,
				Type:     "process_recovered",
				Message:  fmt.Sprintf("감시 프로세스 '%s' 복구됨", wp.Name),
				Details: map[string]interface{}{
					"processName": wp.Name,
				},
				Timestamp: now,
			})
		}

		d.lastPresent[wp.Name] = present
	}

	return alerts
}

// checkHogs 는 CPU/MEM 호그 프로세스를 감지합니다.
func (d *ProcessDetector) checkHogs(now time.Time) []Alert {
	var alerts []Alert

	// 현재 Top CPU 프로세스 중 호그 의심
	currentCPUHogs := make(map[string]float64) // name → cpu%
	for _, p := range d.lastResult.TopCPU {
		if p.CPUPct >= d.config.CPUHogThresholdPct {
			currentCPUHogs[p.Name] = p.CPUPct
		}
	}

	currentMemHogs := make(map[string]float64) // name → mem%
	for _, p := range d.lastResult.TopMem {
		if p.MemPct >= d.config.MemHogThresholdPct {
			currentMemHogs[p.Name] = p.MemPct
		}
	}

	// CPU 호그 추적
	for name, cpuPct := range currentCPUHogs {
		state := d.getOrCreateHogState(name, now)
		state.consecutiveCPU++

		if state.consecutiveCPU >= d.config.HogDurationCount {
			// 이미 알림 발송 → RepeatInterval 경과 시에만 재발행
			if state.alertedCPU && now.Sub(state.lastCPUAlertAt) < RepeatInterval {
				continue
			}
			alerts = append(alerts, Alert{
				ID:       fmt.Sprintf("process-cpu-hog-%s-%d", name, now.Unix()),
				Category: CategoryProcess,
				Severity: SeverityWarning,
				Type:     "cpu_hog",
				Message: fmt.Sprintf("프로세스 '%s' CPU %.1f%% - %d회 연속 고사용률 (루프/과부하 의심)",
					name, cpuPct, state.consecutiveCPU),
				Details: map[string]interface{}{
					"processName":      name,
					"cpuPercent":       cpuPct,
					"consecutiveCount": state.consecutiveCPU,
					"durationSec":      now.Sub(state.firstSeenAt).Seconds(),
				},
				Timestamp: now,
			})
			state.alertedCPU = true
			state.lastCPUAlertAt = now
		}
	}

	// MEM 호그 추적
	for name, memPct := range currentMemHogs {
		state := d.getOrCreateHogState(name, now)
		state.consecutiveMem++

		if state.consecutiveMem >= d.config.HogDurationCount {
			// 이미 알림 발송 → RepeatInterval 경과 시에만 재발행
			if state.alertedMem && now.Sub(state.lastMemAlertAt) < RepeatInterval {
				continue
			}
			alerts = append(alerts, Alert{
				ID:       fmt.Sprintf("process-mem-hog-%s-%d", name, now.Unix()),
				Category: CategoryProcess,
				Severity: SeverityWarning,
				Type:     "mem_hog",
				Message: fmt.Sprintf("프로세스 '%s' MEM %.1f%% - %d회 연속 고사용률 (메모리 누수 의심)",
					name, memPct, state.consecutiveMem),
				Details: map[string]interface{}{
					"processName":      name,
					"memPercent":       memPct,
					"consecutiveCount": state.consecutiveMem,
					"durationSec":      now.Sub(state.firstSeenAt).Seconds(),
				},
				Timestamp: now,
			})
			state.alertedMem = true
			state.lastMemAlertAt = now
		}
	}

	// 호그 해제된 프로세스 리셋
	for name, state := range d.hogStates {
		if _, still := currentCPUHogs[name]; !still {
			state.consecutiveCPU = 0
			state.alertedCPU = false
		}
		if _, still := currentMemHogs[name]; !still {
			state.consecutiveMem = 0
			state.alertedMem = false
		}
	}

	return alerts
}

func (d *ProcessDetector) getOrCreateHogState(name string, now time.Time) *processHogState {
	if state, ok := d.hogStates[name]; ok {
		return state
	}
	state := &processHogState{firstSeenAt: now}
	d.hogStates[name] = state
	return state
}
