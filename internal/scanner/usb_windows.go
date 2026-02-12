//go:build windows

package scanner

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// pnpDeviceResult 는 PowerShell Get-PnpDevice의 JSON 결과를 매핑합니다.
type pnpDeviceResult struct {
	InstanceId   string `json:"InstanceId"`
	FriendlyName string `json:"FriendlyName"`
	Class        string `json:"Class"`
	Manufacturer string `json:"Manufacturer"`
	Status       string `json:"Status"`
}

// scanUSBDevices 는 Windows에서 PowerShell을 사용하여 USB 디바이스를 스캔합니다.
func scanUSBDevices() ([]USBDevice, error) {
	// PowerShell 명령어: USB 버스에 연결된 Present 상태 디바이스 조회
	psCmd := `Get-PnpDevice -PresentOnly | Where-Object { $_.InstanceId -like 'USB\*' } | Select-Object InstanceId, FriendlyName, Class, Manufacturer, Status | ConvertTo-Json -Compress`

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("PowerShell USB 스캔 실패: %w", err)
	}

	outStr := strings.TrimSpace(string(output))
	if outStr == "" {
		return nil, nil
	}

	// PowerShell은 결과가 1개면 배열이 아닌 단일 객체를 반환
	var results []pnpDeviceResult
	if strings.HasPrefix(outStr, "[") {
		if err := json.Unmarshal([]byte(outStr), &results); err != nil {
			return nil, fmt.Errorf("PowerShell 결과 파싱 실패: %w", err)
		}
	} else {
		var single pnpDeviceResult
		if err := json.Unmarshal([]byte(outStr), &single); err != nil {
			return nil, fmt.Errorf("PowerShell 결과 파싱 실패: %w", err)
		}
		results = append(results, single)
	}

	var devices []USBDevice
	for _, r := range results {
		vid, pid := parseInstanceID(r.InstanceId)
		devices = append(devices, USBDevice{
			VendorID:  vid,
			ProductID: pid,
			Vendor:    r.Manufacturer,
			Product:   r.FriendlyName,
			Serial:    extractSerial(r.InstanceId),
			Class:     r.Class,
		})
	}

	return devices, nil
}

// parseInstanceID 는 "USB\VID_8087&PID_0026\5&1234" 형태에서 VendorID, ProductID를 추출합니다.
func parseInstanceID(instanceID string) (vendorID, productID string) {
	parts := strings.Split(instanceID, "\\")
	if len(parts) < 2 {
		return "", ""
	}

	idPart := parts[1] // "VID_8087&PID_0026"
	for _, seg := range strings.Split(idPart, "&") {
		seg = strings.TrimSpace(seg)
		if strings.HasPrefix(seg, "VID_") {
			vendorID = "0x" + strings.TrimPrefix(seg, "VID_")
		}
		if strings.HasPrefix(seg, "PID_") {
			productID = "0x" + strings.TrimPrefix(seg, "PID_")
		}
	}
	return
}

// extractSerial 는 InstanceID의 마지막 세그먼트에서 시리얼 번호를 추출합니다.
func extractSerial(instanceID string) string {
	parts := strings.Split(instanceID, "\\")
	if len(parts) >= 3 {
		serial := parts[2]
		// "&" 포함된 건 자동생성된 ID이므로 시리얼이 아님
		if !strings.Contains(serial, "&") {
			return serial
		}
	}
	return ""
}
