package scanner

// USBDevice 는 연결된 USB 디바이스의 정보를 나타냅니다.
type USBDevice struct {
	VendorID  string `json:"vendorId"`  // 벤더 ID (예: "0x8087")
	ProductID string `json:"productId"` // 제품 ID (예: "0x0026")
	Vendor    string `json:"vendor"`    // 벤더명 (예: "Intel Corp.")
	Product   string `json:"product"`   // 제품명 (예: "AX201 Bluetooth")
	Serial    string `json:"serial"`    // 시리얼 번호 (있으면)
	BusNumber string `json:"busNumber"` // 버스 번호
	DeviceNum string `json:"deviceNum"` // 디바이스 번호
	Class     string `json:"class"`     // USB 클래스 (예: "Mass Storage", "HID")
}

// ScanUSBDevices 는 현재 연결된 USB 디바이스 목록을 반환합니다.
// OS별로 다른 구현이 빌드 태그를 통해 선택됩니다.
//
// - Linux:   /sys/bus/usb/devices 파일시스템 파싱
// - Windows: PowerShell Get-PnpDevice (WMI)
// - macOS:   system_profiler SPUSBDataType 파싱
func ScanUSBDevices() ([]USBDevice, error) {
	return scanUSBDevices()
}
