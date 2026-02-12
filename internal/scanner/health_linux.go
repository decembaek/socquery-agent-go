//go:build linux

package scanner

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func scanHealth() (*HealthInfo, error) {
	info := &HealthInfo{}

	// CPU
	info.CPU.Cores = runtime.NumCPU()
	info.CPU.UsagePercent = getCPUUsageLinux()
	info.CPU.LoadAvg = getLoadAvgLinux()

	// Memory
	info.Memory = getMemoryInfoLinux()

	// Disk (루트 파티션)
	info.Disk = getDiskInfoUnix("/")

	// OS
	info.OS = getOSInfoLinux()

	return info, nil
}

// getCPUUsageLinux 는 /proc/stat을 2회 읽어 CPU 사용률을 계산합니다.
func getCPUUsageLinux() float64 {
	readCPU := func() (idle, total uint64) {
		data, err := os.ReadFile("/proc/stat")
		if err != nil {
			return 0, 0
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "cpu ") {
				fields := strings.Fields(line)
				if len(fields) < 5 {
					return 0, 0
				}
				var sum uint64
				for i := 1; i < len(fields); i++ {
					v, _ := strconv.ParseUint(fields[i], 10, 64)
					sum += v
					if i == 4 { // idle
						idle = v
					}
				}
				return idle, sum
			}
		}
		return 0, 0
	}

	idle1, total1 := readCPU()
	time.Sleep(500 * time.Millisecond)
	idle2, total2 := readCPU()

	idleDelta := float64(idle2 - idle1)
	totalDelta := float64(total2 - total1)

	if totalDelta == 0 {
		return 0
	}

	return round2((1.0 - idleDelta/totalDelta) * 100)
}

// getLoadAvgLinux 는 /proc/loadavg에서 로드 평균을 가져옵니다.
func getLoadAvgLinux() []float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return []float64{0, 0, 0}
	}

	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return []float64{0, 0, 0}
	}

	result := make([]float64, 3)
	for i := 0; i < 3; i++ {
		result[i], _ = strconv.ParseFloat(fields[i], 64)
		result[i] = round2(result[i])
	}
	return result
}

// getMemoryInfoLinux 는 /proc/meminfo에서 메모리 정보를 가져옵니다.
func getMemoryInfoLinux() MemoryInfo {
	var info MemoryInfo

	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return info
	}

	parseKB := func(key string) uint64 {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, key+":") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					v, _ := strconv.ParseUint(fields[1], 10, 64)
					return v // kB
				}
			}
		}
		return 0
	}

	totalKB := parseKB("MemTotal")
	availKB := parseKB("MemAvailable")

	info.TotalMB = totalKB / 1024
	info.AvailableMB = availKB / 1024
	info.UsedMB = info.TotalMB - info.AvailableMB

	if info.TotalMB > 0 {
		info.UsagePercent = round2(float64(info.UsedMB) / float64(info.TotalMB) * 100)
	}

	return info
}

// getDiskInfoUnix 는 syscall로 디스크 사용 정보를 가져옵니다.
func getDiskInfoUnix(path string) DiskInfo {
	var info DiskInfo
	var stat syscall.Statfs_t

	if err := syscall.Statfs(path, &stat); err != nil {
		return info
	}

	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	usedBytes := totalBytes - (stat.Bfree * uint64(stat.Bsize))

	info.TotalGB = round2(float64(totalBytes) / 1024 / 1024 / 1024)
	info.UsedGB = round2(float64(usedBytes) / 1024 / 1024 / 1024)
	info.FreeGB = round2(float64(freeBytes) / 1024 / 1024 / 1024)

	if info.TotalGB > 0 {
		info.UsagePercent = round2(info.UsedGB / info.TotalGB * 100)
	}

	return info
}

// getOSInfoLinux 는 Linux 시스템 정보를 수집합니다.
func getOSInfoLinux() OSInfo {
	info := OSInfo{
		Platform: "linux",
	}

	info.Hostname, _ = os.Hostname()

	// 커널 버전
	if data, err := os.ReadFile("/proc/version"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			info.KernelVersion = fields[2]
		}
	}

	// 업타임
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 1 {
			secs, _ := strconv.ParseFloat(fields[0], 64)
			info.Uptime = uint64(secs)
		}
	}

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
