package anomaly

import (
	"fmt"
	"time"

	"github.com/decembaek/socquery-client-agent/internal/scanner"
)

// USBConfig 는 USB 감지기 설정입니다.
type USBConfig struct {
	// CriticalDevices 는 반드시 존재해야 하는 중요 디바이스 목록입니다.
	// VID:PID 또는 VID:PID:Serial 형태로 지정합니다.
	// 비어 있으면 모든 변경을 info/warning으로 보고합니다.
	CriticalDevices []string
}

// DefaultUSBConfig 는 기본 USB 감지 설정입니다.
func DefaultUSBConfig() USBConfig {
	return USBConfig{
		CriticalDevices: nil, // 사용자가 설정
	}
}

// usbDeviceKey 는 USB 디바이스의 고유 식별자를 생성합니다.
func usbDeviceKey(d scanner.USBDevice) string {
	key := d.VendorID + ":" + d.ProductID
	if d.Serial != "" {
		key += ":" + d.Serial
	}
	return key
}

// USBDetector 는 USB 디바이스 변경을 감지합니다.
// Critical 장치 사라짐 = error, 새 장치 = 구성 변경 이벤트
type USBDetector struct {
	config          USBConfig
	criticalSet     map[string]bool              // 중요 디바이스 키 집합
	previousDevices map[string]scanner.USBDevice  // 이전 스캔의 디바이스 맵 (key → device)
	initialized     bool                         // 최초 스캔 완료 여부
	lastDevices     []scanner.USBDevice
}

// NewUSBDetector 는 새 USB 감지기를 생성합니다.
func NewUSBDetector(config USBConfig) *USBDetector {
	critSet := make(map[string]bool)
	for _, key := range config.CriticalDevices {
		critSet[key] = true
	}

	return &USBDetector{
		config:          config,
		criticalSet:     critSet,
		previousDevices: make(map[string]scanner.USBDevice),
	}
}

func (d *USBDetector) Name() string {
	return "usb"
}

// Feed 는 스캐너에서 받은 USB 디바이스 목록을 감지기에 공급합니다.
func (d *USBDetector) Feed(devices []scanner.USBDevice) {
	d.lastDevices = devices
}

// Check 는 USB 디바이스 변경을 평가합니다.
func (d *USBDetector) Check() []Alert {
	if d.lastDevices == nil {
		return nil
	}

	var alerts []Alert
	now := time.Now()

	// 현재 디바이스 맵 구성
	currentDevices := make(map[string]scanner.USBDevice)
	for _, dev := range d.lastDevices {
		key := usbDeviceKey(dev)
		currentDevices[key] = dev
	}

	// 최초 스캔: 베이스라인 설정만 하고 알림 없음
	if !d.initialized {
		d.previousDevices = currentDevices
		d.initialized = true

		// 중요 디바이스 부재 확인 (최초 스캔에서도)
		for critKey := range d.criticalSet {
			if _, exists := currentDevices[critKey]; !exists {
				alerts = append(alerts, Alert{
					ID:       fmt.Sprintf("usb-critical-missing-%s-%d", critKey, now.Unix()),
					Category: CategoryUSB,
					Severity: SeverityCritical,
					Type:     "critical_device_missing",
					Message:  fmt.Sprintf("중요 USB 디바이스 부재: %s (시작 시점부터 없음)", critKey),
					Details: map[string]interface{}{
						"metric":    "usb",
						"deviceKey": critKey,
						"event":     "missing_at_startup",
					},
					Timestamp: now,
				})
			}
		}
		return alerts
	}

	// --- 사라진 디바이스 감지 ---
	for key, prevDev := range d.previousDevices {
		if _, exists := currentDevices[key]; !exists {
			severity := SeverityWarning
			alertType := "device_removed"

			if d.criticalSet[key] {
				severity = SeverityCritical
				alertType = "critical_device_removed"
			}

			alerts = append(alerts, Alert{
				ID:       fmt.Sprintf("usb-removed-%s-%d", key, now.Unix()),
				Category: CategoryUSB,
				Severity: severity,
				Type:     alertType,
				Message: fmt.Sprintf("USB 디바이스 분리됨: %s %s [%s]",
					prevDev.Vendor, prevDev.Product, key),
				Details: map[string]interface{}{
					"metric":    "usb",
					"deviceKey": key,
					"vendorId":  prevDev.VendorID,
					"productId": prevDev.ProductID,
					"vendor":    prevDev.Vendor,
					"product":   prevDev.Product,
					"serial":    prevDev.Serial,
					"event":     "removed",
					"critical":  d.criticalSet[key],
				},
				Timestamp: now,
			})
		}
	}

	// --- 새로 추가된 디바이스 감지 ---
	for key, curDev := range currentDevices {
		if _, existed := d.previousDevices[key]; !existed {
			severity := SeverityInfo
			alertType := "device_added"

			// 중요 디바이스가 복구된 것일 수 있음
			if d.criticalSet[key] {
				alertType = "critical_device_restored"
			}

			alerts = append(alerts, Alert{
				ID:       fmt.Sprintf("usb-added-%s-%d", key, now.Unix()),
				Category: CategoryUSB,
				Severity: severity,
				Type:     alertType,
				Message: fmt.Sprintf("USB 디바이스 연결됨: %s %s [%s]",
					curDev.Vendor, curDev.Product, key),
				Details: map[string]interface{}{
					"metric":    "usb",
					"deviceKey": key,
					"vendorId":  curDev.VendorID,
					"productId": curDev.ProductID,
					"vendor":    curDev.Vendor,
					"product":   curDev.Product,
					"serial":    curDev.Serial,
					"event":     "added",
				},
				Timestamp: now,
			})
		}
	}

	// 상태 갱신
	d.previousDevices = currentDevices

	return alerts
}
