//go:build darwin

package scanner

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// scanUSBDevices 는 macOS에서 ioreg를 사용하여 USB 디바이스를 스캔합니다.
// system_profiler는 허브 뒤 디바이스를 놓치는 경우가 있어 ioreg를 사용합니다.
func scanUSBDevices() ([]USBDevice, error) {
	cmd := exec.Command("ioreg", "-p", "IOUSB", "-c", "IOUSBHostDevice", "-l")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ioreg USB 스캔 실패: %w", err)
	}

	return parseIoreg(string(output))
}

var (
	reIdVendor     = regexp.MustCompile(`"idVendor"\s*=\s*(\d+)`)
	reIdProduct    = regexp.MustCompile(`"idProduct"\s*=\s*(\d+)`)
	reProductName  = regexp.MustCompile(`"USB Product Name"\s*=\s*"([^"]*)"`)
	reVendorName   = regexp.MustCompile(`"USB Vendor Name"\s*=\s*"([^"]*)"`)
	reSerialNumber = regexp.MustCompile(`"USB Serial Number"\s*=\s*"([^"]*)"`)
	reDeviceClass  = regexp.MustCompile(`"bDeviceClass"\s*=\s*(\d+)`)
	reLocationID   = regexp.MustCompile(`"locationID"\s*=\s*(\d+)`)
	reDeviceEntry  = regexp.MustCompile(`\+-o\s+.+<class IOUSBHostDevice`)
)

// parseIoreg 는 ioreg 출력을 파싱하여 USBDevice 목록으로 변환합니다.
func parseIoreg(output string) ([]USBDevice, error) {
	lines := strings.Split(output, "\n")

	// IOUSBHostDevice 클래스의 블록들만 추출
	blocks := splitByDeviceEntry(lines)

	var devices []USBDevice
	for _, block := range blocks {
		dev := parseDeviceBlock(block)
		// idVendor가 있어야 실제 디바이스
		if dev.VendorID == "" {
			continue
		}
		// Hub(bDeviceClass=9)은 제외
		if dev.Class == "Hub" {
			continue
		}
		devices = append(devices, dev)
	}

	return devices, nil
}

// splitByDeviceEntry 는 IOUSBHostDevice 엔트리 기준으로 블록을 분리합니다.
func splitByDeviceEntry(lines []string) []string {
	var blocks []string
	var current strings.Builder
	inBlock := false

	for _, line := range lines {
		if reDeviceEntry.MatchString(line) {
			// 이전 블록 저장
			if inBlock && current.Len() > 0 {
				blocks = append(blocks, current.String())
				current.Reset()
			}
			inBlock = true
		}
		if inBlock {
			current.WriteString(line)
			current.WriteString("\n")
		}
	}
	// 마지막 블록
	if inBlock && current.Len() > 0 {
		blocks = append(blocks, current.String())
	}

	return blocks
}

// parseDeviceBlock 는 단일 디바이스 블록에서 정보를 추출합니다.
func parseDeviceBlock(block string) USBDevice {
	var dev USBDevice

	// 첫 번째 IOUSBHostDevice 엔트리의 속성만 사용하기 위해
	// 다음 IOUSBHostDevice가 나오면 그 이후 라인은 무시
	lines := strings.Split(block, "\n")
	var relevantBlock strings.Builder
	first := true
	for _, line := range lines {
		if reDeviceEntry.MatchString(line) {
			if first {
				first = false
			} else {
				break // 자식 디바이스 블록 시작 → 무시
			}
		}
		relevantBlock.WriteString(line)
		relevantBlock.WriteString("\n")
	}
	text := relevantBlock.String()

	if m := reIdVendor.FindStringSubmatch(text); len(m) > 1 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			dev.VendorID = fmt.Sprintf("0x%04x", v)
		}
	}

	if m := reIdProduct.FindStringSubmatch(text); len(m) > 1 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			dev.ProductID = fmt.Sprintf("0x%04x", v)
		}
	}

	if m := reVendorName.FindStringSubmatch(text); len(m) > 1 {
		dev.Vendor = m[1]
	}

	if m := reProductName.FindStringSubmatch(text); len(m) > 1 {
		dev.Product = m[1]
	}

	if m := reSerialNumber.FindStringSubmatch(text); len(m) > 1 {
		dev.Serial = m[1]
	}

	if m := reDeviceClass.FindStringSubmatch(text); len(m) > 1 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			dev.Class = mapUSBClassCode(v)
		}
	}

	if m := reLocationID.FindStringSubmatch(text); len(m) > 1 {
		dev.BusNumber = m[1]
	}

	return dev
}

// mapUSBClassCode 는 USB 디바이스 클래스 코드를 이름으로 변환합니다.
func mapUSBClassCode(code int) string {
	classMap := map[int]string{
		0:   "Composite",
		1:   "Audio",
		2:   "Communications",
		3:   "HID",
		5:   "Physical",
		6:   "Image",
		7:   "Printer",
		8:   "Mass Storage",
		9:   "Hub",
		10:  "CDC Data",
		11:  "Smart Card",
		13:  "Content Security",
		14:  "Video",
		15:  "Personal Healthcare",
		16:  "Audio/Video",
		220: "Diagnostic",
		224: "Wireless Controller",
		239: "Miscellaneous",
		254: "Application Specific",
		255: "Vendor Specific",
	}

	if name, ok := classMap[code]; ok {
		return name
	}
	return fmt.Sprintf("Unknown (%d)", code)
}
