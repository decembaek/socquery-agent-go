//go:build darwin || linux

package scanner

import (
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

const topN = 10

func scanProcesses() (*ProcessScanResult, error) {
	// ps 명령으로 모든 프로세스 정보 조회
	// pid, user, %cpu, %mem, rss(KB), state, lstart(시작시간), command
	cmd := exec.Command("ps", "axo", "pid,user,%cpu,%mem,rss,state,comm")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return &ProcessScanResult{}, nil
	}

	var procs []ProcessInfo

	// 첫 줄은 헤더, 건너뜀
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}

		pid, _ := strconv.Atoi(fields[0])
		cpuPct, _ := strconv.ParseFloat(fields[2], 64)
		memPct, _ := strconv.ParseFloat(fields[3], 64)
		rssKB, _ := strconv.ParseFloat(fields[4], 64)

		state := mapProcessState(fields[5])

		// comm은 마지막 필드 (경로 포함 가능하므로 나머지 전부 합침)
		name := strings.Join(fields[6:], " ")
		// 경로에서 바이너리명만 추출
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}

		procs = append(procs, ProcessInfo{
			PID:    pid,
			Name:   name,
			User:   fields[1],
			CPUPct: cpuPct,
			MemPct: memPct,
			MemMB:  float64(int(rssKB/1024*100)) / 100,
			Status: state,
		})
	}

	// Top CPU
	sort.Slice(procs, func(i, j int) bool {
		return procs[i].CPUPct > procs[j].CPUPct
	})
	topCPU := take(procs, topN)

	// Top Memory
	sort.Slice(procs, func(i, j int) bool {
		return procs[i].MemPct > procs[j].MemPct
	})
	topMem := take(procs, topN)

	return &ProcessScanResult{
		TotalCount: len(procs),
		TopCPU:     topCPU,
		TopMem:     topMem,
	}, nil
}

// mapProcessState 는 ps의 state 코드를 이름으로 변환합니다.
func mapProcessState(state string) string {
	if len(state) == 0 {
		return "unknown"
	}
	switch state[0] {
	case 'R':
		return "running"
	case 'S':
		return "sleeping"
	case 'D':
		return "disk_sleep"
	case 'T':
		return "stopped"
	case 'Z':
		return "zombie"
	case 'I':
		return "idle"
	case 'U':
		return "uninterruptible"
	default:
		return string(state[0])
	}
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
