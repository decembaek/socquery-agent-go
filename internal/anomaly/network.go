package anomaly

import (
	"fmt"
	"sync"
	"time"

	"github.com/decembaek/socquery-client-agent/internal/scanner"
)

// NetworkConfig is the network detector configuration.
// Only DNS and MQTT disconnect checks are included; low-level metrics like CLOSE_WAIT are omitted for user clarity.
type NetworkConfig struct {
	// DNS consecutive failure thresholds (warning after 3, critical after 5)
	DNSFailWarningCount  int
	DNSFailCriticalCount int

	// MQTT disconnect frequency (warning/critical within window)
	MQTTDisconnectWindowMin int
	MQTTDisconnectWarning   int
	MQTTDisconnectCritical  int
}

// DefaultNetworkConfig returns default network detector config.
func DefaultNetworkConfig() NetworkConfig {
	return NetworkConfig{
		DNSFailWarningCount:     3,
		DNSFailCriticalCount:    5,
		MQTTDisconnectWindowMin: 10,
		MQTTDisconnectWarning:   3,
		MQTTDisconnectCritical:  5,
	}
}

// NetworkDetector 는 네트워크/MQTT 이상치를 감지합니다.
type NetworkDetector struct {
	config NetworkConfig

	// DNS 상태 추적
	dnsFailCount    int  // 연속 실패 횟수
	dnsAlerted      bool
	dnsLastSeverity Severity
	dnsLastAlertAt  time.Time

	// MQTT disconnect tracking (NotifyMQTTDisconnect from protocol)
	mu                sync.Mutex
	mqttDisconnectLog []time.Time
	mqttAlerted      bool
	mqttLastSeverity  Severity
	mqttLastAlertAt   time.Time

	lastResult *scanner.NetworkScanResult
}

// NewNetworkDetector 는 새 네트워크 감지기를 생성합니다.
func NewNetworkDetector(config NetworkConfig) *NetworkDetector {
	return &NetworkDetector{
		config:            config,
		mqttDisconnectLog: make([]time.Time, 0),
	}
}

func (d *NetworkDetector) Name() string {
	return "network"
}

// Feed 는 스캐너에서 받은 네트워크 결과를 감지기에 공급합니다.
func (d *NetworkDetector) Feed(result *scanner.NetworkScanResult) {
	d.lastResult = result
}

// NotifyMQTTDisconnect 는 MQTT 연결 끊김을 통보합니다.
// protocol 패키지의 재연결 콜백에서 호출합니다.
func (d *NetworkDetector) NotifyMQTTDisconnect() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mqttDisconnectLog = append(d.mqttDisconnectLog, time.Now())
}

// Check evaluates network anomalies (DNS, MQTT only).
func (d *NetworkDetector) Check() []Alert {
	var alerts []Alert
	now := time.Now()

	if d.lastResult != nil {
		alerts = append(alerts, d.checkDNS(now)...)
	}
	alerts = append(alerts, d.checkMQTTDisconnects(now)...)

	return alerts
}

// checkDNS 는 DNS 연속 실패를 감지합니다.
func (d *NetworkDetector) checkDNS(now time.Time) []Alert {
	var alerts []Alert

	if d.lastResult.DNS.Reachable {
		// 복구
		if d.dnsAlerted {
			alerts = append(alerts, Alert{
				ID:       fmt.Sprintf("network-dns-recovered-%d", now.Unix()),
				Category: CategoryNetwork,
				Severity: SeverityInfo,
				Type:     "dns_recovered",
				Message:  fmt.Sprintf("DNS 연결 복구 (응답시간: %dms)", d.lastResult.DNS.LatencyMs),
				Details: map[string]interface{}{
					"server":    d.lastResult.DNS.Server,
					"latencyMs": d.lastResult.DNS.LatencyMs,
				},
				Timestamp: now,
			})
		}
		d.dnsFailCount = 0
		d.dnsAlerted = false
		d.dnsLastSeverity = ""
		return alerts
	}

	// DNS 실패
	d.dnsFailCount++

	var severity Severity
	if d.dnsFailCount >= d.config.DNSFailCriticalCount {
		severity = SeverityCritical
	} else if d.dnsFailCount >= d.config.DNSFailWarningCount {
		severity = SeverityWarning
	} else {
		return nil // 아직 임계치 미달
	}

	// 같은 심각도로 이미 알림 → RepeatInterval 경과 시에만 재발행
	if d.dnsAlerted && d.dnsLastSeverity == severity {
		if now.Sub(d.dnsLastAlertAt) < RepeatInterval {
			return nil
		}
	}

	alerts = append(alerts, Alert{
		ID:       fmt.Sprintf("network-dns-fail-%d", now.Unix()),
		Category: CategoryNetwork,
		Severity: severity,
		Type:     "dns_failure",
		Message:  fmt.Sprintf("DNS 연결 %d회 연속 실패 (서버: %s)", d.dnsFailCount, d.lastResult.DNS.Server),
		Details: map[string]interface{}{
			"server":         d.lastResult.DNS.Server,
			"failCount":      d.dnsFailCount,
			"warningThreshold":  d.config.DNSFailWarningCount,
			"criticalThreshold": d.config.DNSFailCriticalCount,
		},
		Timestamp: now,
	})

	d.dnsAlerted = true
	d.dnsLastSeverity = severity
	d.dnsLastAlertAt = now
	return alerts
}

// checkMQTTDisconnects 는 MQTT 끊김 빈도를 감지합니다.
func (d *NetworkDetector) checkMQTTDisconnects(now time.Time) []Alert {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 윈도우 밖의 이벤트 제거
	window := time.Duration(d.config.MQTTDisconnectWindowMin) * time.Minute
	cutoff := now.Add(-window)
	filtered := make([]time.Time, 0, len(d.mqttDisconnectLog))
	for _, t := range d.mqttDisconnectLog {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	d.mqttDisconnectLog = filtered

	count := len(filtered)

	var alerts []Alert

	if count == 0 {
		if d.mqttAlerted {
			// 윈도우 내 끊김 없음 → 복구
			d.mqttAlerted = false
			d.mqttLastSeverity = ""
		}
		return nil
	}

	var severity Severity
	if count >= d.config.MQTTDisconnectCritical {
		severity = SeverityCritical
	} else if count >= d.config.MQTTDisconnectWarning {
		severity = SeverityWarning
	} else {
		return nil
	}

	if d.mqttAlerted && d.mqttLastSeverity == severity {
		if now.Sub(d.mqttLastAlertAt) < RepeatInterval {
			return nil
		}
	}

	alerts = append(alerts, Alert{
		ID:       fmt.Sprintf("network-mqtt-disconnect-%d", now.Unix()),
		Category: CategoryNetwork,
		Severity: severity,
		Type:     "mqtt_disconnect_freq",
		Message: fmt.Sprintf("MQTT 연결 끊김 %d회 (최근 %d분 내)",
			count, d.config.MQTTDisconnectWindowMin),
		Details: map[string]interface{}{
			"disconnectCount": count,
			"windowMinutes":   d.config.MQTTDisconnectWindowMin,
		},
		Timestamp: now,
	})

	d.mqttAlerted = true
	d.mqttLastSeverity = severity
	d.mqttLastAlertAt = now
	return alerts
}
