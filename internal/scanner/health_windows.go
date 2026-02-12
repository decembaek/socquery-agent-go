//go:build windows

package scanner

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func scanHealth() (*HealthInfo, error) {
	info := &HealthInfo{}

	// CPU
	info.CPU.Cores = runtime.NumCPU()
	info.CPU.UsagePercent = getCPUUsageWindows()
	info.CPU.LoadAvg = []float64{0, 0, 0} // Windows는 Load Average 미지원

	// Memory
	info.Memory = getMemoryInfoWindows()

	// Disk (C: 드라이브)
	info.Disk = getDiskInfoWindows("C:\\")

	// OS
	info.OS = getOSInfoWindows()

	return info, nil
}

// getCPUUsageWindows 는 PowerShell로 CPU 사용률을 가져옵니다.
func getCPUUsageWindows() float64 {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average).Average")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	usage, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0
	}
	return round2(usage)
}

// MEMORYSTATUSEX 는 Windows 메모리 상태 구조체입니다.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// getMemoryInfoWindows 는 GlobalMemoryStatusEx API로 메모리 정보를 가져옵니다.
func getMemoryInfoWindows() MemoryInfo {
	var info MemoryInfo

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	globalMemoryStatusEx := kernel32.NewProc("GlobalMemoryStatusEx")

	var memStatus memoryStatusEx
	memStatus.Length = uint32(unsafe.Sizeof(memStatus))

	ret, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memStatus)))
	if ret == 0 {
		return info
	}

	info.TotalMB = memStatus.TotalPhys / 1024 / 1024
	info.AvailableMB = memStatus.AvailPhys / 1024 / 1024
	info.UsedMB = info.TotalMB - info.AvailableMB

	if info.TotalMB > 0 {
		info.UsagePercent = round2(float64(info.UsedMB) / float64(info.TotalMB) * 100)
	}

	return info
}

// getDiskInfoWindows 는 GetDiskFreeSpaceEx API로 디스크 정보를 가져옵니다.
func getDiskInfoWindows(path string) DiskInfo {
	var info DiskInfo

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	pathPtr, _ := syscall.UTF16PtrFromString(path)
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64

	ret, _, _ := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if ret == 0 {
		return info
	}

	info.TotalGB = round2(float64(totalBytes) / 1024 / 1024 / 1024)
	info.FreeGB = round2(float64(freeBytesAvailable) / 1024 / 1024 / 1024)
	info.UsedGB = round2(info.TotalGB - info.FreeGB)

	if info.TotalGB > 0 {
		info.UsagePercent = round2(info.UsedGB / info.TotalGB * 100)
	}

	return info
}

// getOSInfoWindows 는 Windows 시스템 정보를 수집합니다.
func getOSInfoWindows() OSInfo {
	info := OSInfo{
		Platform: "windows",
	}

	info.Hostname, _ = os.Hostname()

	// Windows 버전
	cmd := exec.Command("cmd", "/C", "ver")
	if output, err := cmd.Output(); err == nil {
		info.KernelVersion = strings.TrimSpace(string(output))
	}

	// 업타임 (밀리초 → 초)
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getTickCount64 := kernel32.NewProc("GetTickCount64")
	ret, _, _ := getTickCount64.Call()
	info.Uptime = uint64(ret) / 1000

	return info
}

func round2(f float64) float64 {
	return float64(int(f*100)) / 100
}

func formatUptime(seconds uint64) string {
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm %ds", days, hours, minutes, secs)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, secs)
	}
	return fmt.Sprintf("%dm %ds", minutes, secs)
}

// unused import guard
var _ = time.Now
