package anomaly

import (
	"fmt"
	"time"

	"github.com/decembaek/socquery-agent-go/internal/scanner"
)

// ResourceThresholds 는 리소스 임계치 설정입니다.
type ResourceThresholds struct {
	// CPU
	CPUWarningPct  float64 // CPU 경고 임계치 (%, 기본 85)
	CPUCriticalPct float64 // CPU 심각 임계치 (%, 기본 95)
	CPUDurationSec int     // CPU 지속 조건 (연속 N회 초과 시 발동, 기본 3)

	// Memory
	MemWarningPct  float64 // 메모리 경고 임계치 (%, 기본 85)
	MemCriticalPct float64 // 메모리 심각 임계치 (%, 기본 95)
	MemDurationSec int     // 메모리 지속 조건 (연속 N회, 기본 5)

	// Disk
	DiskWarningPct  float64 // 디스크 경고 임계치 (%, 기본 90)
	DiskCriticalPct float64 // 디스크 심각 임계치 (%, 기본 95)
}

// DefaultResourceThresholds 는 기본 리소스 임계치입니다.
func DefaultResourceThresholds() ResourceThresholds {
	return ResourceThresholds{
		CPUWarningPct:   85,
		CPUCriticalPct:  95,
		CPUDurationSec:  3, // 3회 연속 (health 스캔 30초 주기면 ~90초)
		MemWarningPct:   85,
		MemCriticalPct:  95,
		MemDurationSec:  5, // 5회 연속 (~150초)
		DiskWarningPct:  90,
		DiskCriticalPct: 95,
	}
}

// RepeatInterval 은 같은 심각도에서 알림을 재발행하는 최소 간격입니다.
const RepeatInterval = 5 * time.Minute

// resourceState 는 리소스별 연속 초과 카운터입니다.
type resourceState struct {
	consecutiveCount int       // 연속 초과 횟수
	firstExceedAt    time.Time // 처음 초과 시작 시각
	lastSeverity     Severity  // 마지막 발동 심각도 (중복 방지)
	alerted          bool      // 이미 알림 발송됨
	lastAlertAt      time.Time // 마지막 알림 발송 시각 (재발행 판단용)
}

// ResourceDetector 는 CPU/메모리/디스크 이상치를 감지합니다.
// 고정 임계치 + 연속 조건으로 스파이크와 지속 고부하를 구분합니다.
type ResourceDetector struct {
	thresholds ResourceThresholds
	cpuState   resourceState
	memState   resourceState
	diskState  resourceState // 디스크는 연속 조건 없이 즉시 (느리게 변하므로)
	lastHealth *scanner.HealthInfo
}

// NewResourceDetector 는 새 리소스 감지기를 생성합니다.
func NewResourceDetector(thresholds ResourceThresholds) *ResourceDetector {
	return &ResourceDetector{
		thresholds: thresholds,
	}
}

func (d *ResourceDetector) Name() string {
	return "resource"
}

// Feed 는 스캐너에서 받은 HealthInfo를 감지기에 공급합니다.
func (d *ResourceDetector) Feed(info *scanner.HealthInfo) {
	d.lastHealth = info
}

// Check 는 현재 상태를 평가하여 이상치 Alert를 반환합니다.
func (d *ResourceDetector) Check() []Alert {
	if d.lastHealth == nil {
		return nil
	}

	var alerts []Alert
	now := time.Now()

	// --- CPU ---
	cpuAlerts := d.checkMetric(
		d.lastHealth.CPU.UsagePercent,
		d.thresholds.CPUWarningPct,
		d.thresholds.CPUCriticalPct,
		d.thresholds.CPUDurationSec,
		&d.cpuState,
		"cpu",
		"CPU 사용률",
		now,
	)
	alerts = append(alerts, cpuAlerts...)

	// --- Memory ---
	memAlerts := d.checkMetric(
		d.lastHealth.Memory.UsagePercent,
		d.thresholds.MemWarningPct,
		d.thresholds.MemCriticalPct,
		d.thresholds.MemDurationSec,
		&d.memState,
		"memory",
		"메모리 사용률",
		now,
	)
	alerts = append(alerts, memAlerts...)

	// --- Disk (연속 조건 없이 즉시) ---
	diskAlerts := d.checkDisk(now)
	alerts = append(alerts, diskAlerts...)

	return alerts
}

// checkMetric 는 연속 조건 기반 메트릭 검사를 수행합니다.
func (d *ResourceDetector) checkMetric(
	current float64,
	warnThreshold float64,
	critThreshold float64,
	durationCount int,
	state *resourceState,
	metricType string,
	metricLabel string,
	now time.Time,
) []Alert {
	var alerts []Alert

	// 임계치 미달 → 상태 리셋
	if current < warnThreshold {
		if state.alerted {
			// 복구 알림
			alerts = append(alerts, Alert{
				ID:       fmt.Sprintf("resource-%s-recovered-%d", metricType, now.Unix()),
				Category: CategoryResource,
				Severity: SeverityInfo,
				Type:     metricType + "_recovered",
				Message:  fmt.Sprintf("%s 정상 복구 (%.1f%%)", metricLabel, current),
				Details: map[string]interface{}{
					"metric":  metricType,
					"current": current,
				},
				Timestamp: now,
			})
		}
		state.consecutiveCount = 0
		state.alerted = false
		state.lastSeverity = ""
		return alerts
	}

	// 임계치 초과 → 카운터 증가
	if state.consecutiveCount == 0 {
		state.firstExceedAt = now
	}
	state.consecutiveCount++

	// 심각도 결정
	var severity Severity
	if current >= critThreshold {
		severity = SeverityCritical
	} else {
		severity = SeverityWarning
	}

	// 연속 조건 충족 확인
	if state.consecutiveCount >= durationCount {
		// 이미 같은 심각도로 알림을 보냈으면 → RepeatInterval 경과 시에만 재발행
		if state.alerted && state.lastSeverity == severity {
			if now.Sub(state.lastAlertAt) < RepeatInterval {
				return nil
			}
		}

		alertType := metricType + "_sustained"
		if state.consecutiveCount == 1 {
			alertType = metricType + "_spike"
		}

		alerts = append(alerts, Alert{
			ID:       fmt.Sprintf("resource-%s-%d", metricType, now.Unix()),
			Category: CategoryResource,
			Severity: severity,
			Type:     alertType,
			Message: fmt.Sprintf("%s %.1f%% - %d회 연속 초과 (%.0f초 지속)",
				metricLabel, current, state.consecutiveCount,
				now.Sub(state.firstExceedAt).Seconds()),
			Details: map[string]interface{}{
				"metric":           metricType,
				"current":          current,
				"threshold":        warnThreshold,
				"consecutiveCount": state.consecutiveCount,
				"durationSec":      now.Sub(state.firstExceedAt).Seconds(),
			},
			Timestamp: now,
		})

		state.alerted = true
		state.lastSeverity = severity
		state.lastAlertAt = now
	}

	return alerts
}

// checkDisk 는 디스크 사용률을 검사합니다. (느리게 변하므로 연속 조건 없이 즉시)
func (d *ResourceDetector) checkDisk(now time.Time) []Alert {
	var alerts []Alert
	current := d.lastHealth.Disk.UsagePercent

	if current < d.thresholds.DiskWarningPct {
		if d.diskState.alerted {
			alerts = append(alerts, Alert{
				ID:       fmt.Sprintf("resource-disk-recovered-%d", now.Unix()),
				Category: CategoryResource,
				Severity: SeverityInfo,
				Type:     "disk_recovered",
				Message:  fmt.Sprintf("디스크 사용률 정상 복구 (%.1f%%)", current),
				Details: map[string]interface{}{
					"metric":  "disk",
					"current": current,
				},
				Timestamp: now,
			})
			d.diskState.alerted = false
			d.diskState.lastSeverity = ""
		}
		return alerts
	}

	var severity Severity
	if current >= d.thresholds.DiskCriticalPct {
		severity = SeverityCritical
	} else {
		severity = SeverityWarning
	}

	// 심각도가 변하지 않았으면 → RepeatInterval 경과 시에만 재발행
	if d.diskState.alerted && d.diskState.lastSeverity == severity {
		if now.Sub(d.diskState.lastAlertAt) < RepeatInterval {
			return nil
		}
	}

	freeGB := d.lastHealth.Disk.FreeGB
	alerts = append(alerts, Alert{
		ID:       fmt.Sprintf("resource-disk-%d", now.Unix()),
		Category: CategoryResource,
		Severity: severity,
		Type:     "disk_usage",
		Message:  fmt.Sprintf("디스크 사용률 %.1f%% (잔여 %.1f GB)", current, freeGB),
		Details: map[string]interface{}{
			"metric":  "disk",
			"current": current,
			"freeGB":  freeGB,
			"totalGB": d.lastHealth.Disk.TotalGB,
		},
		Timestamp: now,
	})

	d.diskState.alerted = true
	d.diskState.lastSeverity = severity
	d.diskState.lastAlertAt = now

	return alerts
}
