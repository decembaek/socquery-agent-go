//go:build windows

package scanner

import (
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

const topN = 10

func scanProcesses() (*ProcessScanResult, error) {
	// PowerShell로 프로세스 목록 조회 (CSV 형태)
	psCmd := `Get-Process | Where-Object { $_.ProcessName -ne 'Idle' } | ` +
		`Select-Object Id,ProcessName,` +
		`@{N='CPU';E={[math]::Round($_.CPU,2)}},` +
		`@{N='MemMB';E={[math]::Round($_.WorkingSet64/1MB,2)}},` +
		`@{N='Status';E={if($_.Responding){'running'}else{'not_responding'}}} | ` +
		`ConvertTo-Csv -NoTypeInformation`

	cmd := exec.Command("powershell", "-NoProfile", "-Command", psCmd)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return &ProcessScanResult{}, nil
	}

	// 전체 메모리 조회 (% 계산용)
	totalMemMB := getTotalMemoryMBWindows()

	var procs []ProcessInfo
	for _, line := range lines[1:] { // 첫 줄은 CSV 헤더
		line = strings.TrimSpace(line)
		fields := parseCSVLine(line)
		if len(fields) < 5 {
			continue
		}

		pid, _ := strconv.Atoi(fields[0])
		cpuSec, _ := strconv.ParseFloat(fields[2], 64)
		memMB, _ := strconv.ParseFloat(fields[3], 64)

		var memPct float64
		if totalMemMB > 0 {
			memPct = float64(int(memMB/totalMemMB*10000)) / 100
		}

		procs = append(procs, ProcessInfo{
			PID:    pid,
			Name:   fields[1],
			User:   "", // Windows Get-Process에서 user 정보 기본 미포함
			CPUPct: cpuSec,
			MemPct: memPct,
			MemMB:  memMB,
			Status: fields[4],
		})
	}

	// Top CPU
	sort.Slice(procs, func(i, j int) bool {
		return procs[i].CPUPct > procs[j].CPUPct
	})
	topCPU := take(procs, topN)

	// Top Memory
	sort.Slice(procs, func(i, j int) bool {
		return procs[i].MemMB > procs[j].MemMB
	})
	topMem := take(procs, topN)

	return &ProcessScanResult{
		TotalCount: len(procs),
		TopCPU:     topCPU,
		TopMem:     topMem,
	}, nil
}

// parseCSVLine 는 간단한 CSV 라인을 파싱합니다 (쌍따옴표 제거).
func parseCSVLine(line string) []string {
	fields := strings.Split(line, ",")
	for i, f := range fields {
		fields[i] = strings.Trim(strings.TrimSpace(f), "\"")
	}
	return fields
}

// getTotalMemoryMBWindows 는 전체 물리 메모리(MB)를 조회합니다.
func getTotalMemoryMBWindows() float64 {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"[math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory/1MB,0)")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	return v
}

func take(procs []ProcessInfo, n int) []ProcessInfo {
	if len(procs) <= n {
		result := make([]ProcessInfo, len(procs))
		copy(result, procs)
		return result
	}
	result := make([]ProcessInfo, n)
	copy(result, procs[:n])
	return result
}
