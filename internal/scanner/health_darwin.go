//go:build darwin

package scanner

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
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
	info.CPU.UsagePercent = getCPUUsageDarwin()
	info.CPU.LoadAvg = getLoadAvgDarwin()

	// Memory
	info.Memory = getMemoryInfoDarwin()

	// Disk (루트 파티션)
	info.Disk = getDiskInfoUnix("/")

	// OS
	info.OS = getOSInfoDarwin()

	return info, nil
}

// getCPUUsageDarwin 는 top 명령으로 CPU 사용률을 가져옵니다.
func getCPUUsageDarwin() float64 {
	cmd := exec.Command("top", "-l", "1", "-n", "0", "-s", "0")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	// "CPU usage: 12.34% user, 5.67% sys, 81.99% idle"
	re := regexp.MustCompile(`CPU usage:\s+([\d.]+)%\s+user,\s+([\d.]+)%\s+sys,\s+([\d.]+)%\s+idle`)
	m := re.FindStringSubmatch(string(output))
	if len(m) < 4 {
		return 0
	}

	user, _ := strconv.ParseFloat(m[1], 64)
	sys, _ := strconv.ParseFloat(m[2], 64)
	return round2(user + sys)
}

// getLoadAvgDarwin 는 sysctl로 로드 평균을 가져옵니다.
func getLoadAvgDarwin() []float64 {
	cmd := exec.Command("sysctl", "-n", "vm.loadavg")
	output, err := cmd.Output()
	if err != nil {
		return []float64{0, 0, 0}
	}

	// "{ 1.23 0.89 0.67 }"
	str := strings.Trim(strings.TrimSpace(string(output)), "{ }")
	fields := strings.Fields(str)
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

// getMemoryInfoDarwin 는 sysctl과 vm_stat으로 메모리 정보를 가져옵니다.
func getMemoryInfoDarwin() MemoryInfo {
	var info MemoryInfo

	// 전체 메모리 (sysctl)
	cmd := exec.Command("sysctl", "-n", "hw.memsize")
	output, err := cmd.Output()
	if err == nil {
		bytes, _ := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
		info.TotalMB = bytes / 1024 / 1024
	}

	// vm_stat으로 페이지 통계
	cmd = exec.Command("vm_stat")
	output, err = cmd.Output()
	if err != nil {
		return info
	}

	pageSize := uint64(16384) // Apple Silicon 기본 16KB
	// 페이지 크기 확인
	rePage := regexp.MustCompile(`page size of (\d+) bytes`)
	if m := rePage.FindStringSubmatch(string(output)); len(m) > 1 {
		if ps, err := strconv.ParseUint(m[1], 10, 64); err == nil {
			pageSize = ps
		}
	}

	parsePages := func(key string) uint64 {
		re := regexp.MustCompile(key + `:\s+(\d+)`)
		if m := re.FindStringSubmatch(string(output)); len(m) > 1 {
			v, _ := strconv.ParseUint(m[1], 10, 64)
			return v
		}
		return 0
	}

	free := parsePages("Pages free")
	active := parsePages("Pages active")
	inactive := parsePages("Pages inactive")
	speculative := parsePages("Pages speculative")
	wired := parsePages("Pages wired down")
	compressed := parsePages("Pages occupied by compressor")
	_ = inactive
	_ = speculative

	usedPages := active + wired + compressed
	freePages := free

	info.UsedMB = (usedPages * pageSize) / 1024 / 1024
	info.AvailableMB = (freePages * pageSize) / 1024 / 1024

	if info.TotalMB > 0 {
		info.UsagePercent = round2(float64(info.UsedMB) / float64(info.TotalMB) * 100)
	}

	return info
}

// getDiskInfoUnix 는 syscall로 디스크 사용 정보를 가져옵니다 (darwin/linux 공통).
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

// getOSInfoDarwin 는 macOS 시스템 정보를 수집합니다.
func getOSInfoDarwin() OSInfo {
	info := OSInfo{
		Platform: "darwin",
	}

	info.Hostname, _ = os.Hostname()

	// 커널 버전
	cmd := exec.Command("uname", "-r")
	if output, err := cmd.Output(); err == nil {
		info.KernelVersion = strings.TrimSpace(string(output))
	}

	// 업타임 (sysctl kern.boottime)
	cmd = exec.Command("sysctl", "-n", "kern.boottime")
	if output, err := cmd.Output(); err == nil {
		// "{ sec = 1707400000, usec = 0 }"
		re := regexp.MustCompile(`sec\s*=\s*(\d+)`)
		if m := re.FindStringSubmatch(string(output)); len(m) > 1 {
			bootSec, _ := strconv.ParseInt(m[1], 10, 64)
			info.Uptime = uint64(time.Now().Unix() - bootSec)
		}
	}

	return info
}

func round2(f float64) float64 {
	return float64(int(f*100)) / 100
}

// formatUptime 은 초를 "Xd Xh Xm Xs" 형식으로 변환합니다 (디버그용).
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
