//go:build linux

package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const sysUSBPath = "/sys/bus/usb/devices"

// scanUSBDevices 는 Linux에서 /sys/bus/usb/devices를 파싱하여 USB 디바이스를 스캔합니다.
func scanUSBDevices() ([]USBDevice, error) {
	entries, err := os.ReadDir(sysUSBPath)
	if err != nil {
		return nil, fmt.Errorf("USB 디바이스 경로 읽기 실패: %w", err)
	}

	var devices []USBDevice

	for _, entry := range entries {
		devPath := filepath.Join(sysUSBPath, entry.Name())

		// idVendor 파일이 있는 디렉토리만 실제 USB 디바이스
		vendorID := readSysFile(filepath.Join(devPath, "idVendor"))
		if vendorID == "" {
			continue
		}

		productID := readSysFile(filepath.Join(devPath, "idProduct"))
		vendor := readSysFile(filepath.Join(devPath, "manufacturer"))
		product := readSysFile(filepath.Join(devPath, "product"))
		serial := readSysFile(filepath.Join(devPath, "serial"))
		busNum := readSysFile(filepath.Join(devPath, "busnum"))
		devNum := readSysFile(filepath.Join(devPath, "devnum"))
		devClass := readSysFile(filepath.Join(devPath, "bDeviceClass"))

		devices = append(devices, USBDevice{
			VendorID:  "0x" + vendorID,
			ProductID: "0x" + productID,
			Vendor:    vendor,
			Product:   product,
			Serial:    serial,
			BusNumber: busNum,
			DeviceNum: devNum,
			Class:     mapUSBClass(devClass),
		})
	}

	return devices, nil
}

// readSysFile 는 sysfs 파일을 읽어 트림된 문자열을 반환합니다.
func readSysFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// mapUSBClass 는 USB 디바이스 클래스 코드를 사람이 읽을 수 있는 이름으로 변환합니다.
func mapUSBClass(classCode string) string {
	classMap := map[string]string{
		"00": "Composite",
		"01": "Audio",
		"02": "Communications",
		"03": "HID",
		"05": "Physical",
		"06": "Image",
		"07": "Printer",
		"08": "Mass Storage",
		"09": "Hub",
		"0a": "CDC Data",
		"0b": "Smart Card",
		"0d": "Content Security",
		"0e": "Video",
		"0f": "Personal Healthcare",
		"10": "Audio/Video",
		"11": "Billboard",
		"12": "USB Type-C Bridge",
		"dc": "Diagnostic",
		"e0": "Wireless Controller",
		"ef": "Miscellaneous",
		"fe": "Application Specific",
		"ff": "Vendor Specific",
	}

	code := strings.ToLower(strings.TrimSpace(classCode))
	if name, ok := classMap[code]; ok {
		return name
	}
	if classCode == "" {
		return ""
	}
	return "Unknown (" + classCode + ")"
}
