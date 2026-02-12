package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/decembaek/socquery-agent-go/internal/anomaly"
	"github.com/decembaek/socquery-agent-go/internal/config"
	"github.com/decembaek/socquery-agent-go/internal/executor"
	"github.com/decembaek/socquery-agent-go/internal/protocol"
	"github.com/decembaek/socquery-agent-go/internal/scanner"
)

const (
	// Scan intervals
	usbScanInterval     = 15 * time.Second // Short interval so USB add/remove is reported quickly
	healthScanInterval  = 30 * time.Second
	processScanInterval = 60 * time.Second
	networkScanInterval = 60 * time.Second
)

// 전역 anomaly 엔진 (스캔 함수들에서 접근)
var anomalyEngine *anomaly.Engine

func main() {
	// CLI 플래그 파싱
	token := flag.String("token", "", "장비 등록 토큰 (최초 등록 시 필수)")
	scanUSB := flag.Bool("scan-usb", false, "USB 디바이스 1회 스캔 후 종료 (테스트용)")
	scanHealth := flag.Bool("scan-health", false, "OS 헬스 1회 스캔 후 종료 (테스트용)")
	scanProcess := flag.Bool("scan-process", false, "프로세스 1회 스캔 후 종료 (테스트용)")
	scanNetwork := flag.Bool("scan-network", false, "네트워크 1회 스캔 후 종료 (테스트용)")
	scanAll := flag.Bool("scan-all", false, "전체 스캔 1회 실행 후 종료 (테스트용)")
	detectTest := flag.Bool("detect-test", false, "이상치 감지 테스트 (1회 전체 스캔 + 분석)")
	httpTest := flag.String("http-test", "", "HTTP 프록시 테스트 (URL 지정, GET 요청 실행)")
	flag.Parse()

	// 테스트 모드: 1회 스캔 후 종료
	switch {
	case *scanAll:
		runAllScanOnce()
		return
	case *scanUSB:
		runUSBScanOnce()
		return
	case *scanHealth:
		runHealthScanOnce()
		return
	case *scanProcess:
		runProcessScanOnce()
		return
	case *scanNetwork:
		runNetworkScanOnce()
		return
	case *detectTest:
		runDetectTest()
		return
	case *httpTest != "":
		runHTTPTest(*httpTest)
		return
	}

	fmt.Println("=== SocQuery Agent 시작 ===")

	// 1. 설정 로드 또는 서버 등록
	cfg, err := config.LoadOrRegister(*token)
	if err != nil {
		log.Fatalf("[에러] %v\n", err)
	}

	fmt.Printf("[정보] ThingName: %s\n", cfg.ThingName)
	fmt.Printf("[정보] TenantID:  %s\n", cfg.TenantID)
	fmt.Printf("[정보] Region:    %s\n", cfg.Region)
	fmt.Printf("[정보] Endpoint:  %s\n", cfg.IotEndpoint)

	// 토픽 베이스: socquery/{tenant_id}/{thingName}
	topicBase := fmt.Sprintf("socquery/%s/%s", cfg.TenantID, cfg.ThingName)

	// 2. 명령 실행기(Executor Router) 생성
	cmdRouter := executor.NewRouter()

	// 3. MQTT 클라이언트 생성 (명령 수신 → Router로 전달)
	mqttClient, err := protocol.NewMQTTClient(cfg, func(topic string, payload []byte) {
		cmdRouter.HandleCommand(payload)
	})
	if err != nil {
		log.Fatalf("[에러] MQTT 클라이언트 생성 실패: %v\n", err)
	}

	// 4. AWS IoT Core 연결
	if err := mqttClient.Connect(); err != nil {
		log.Fatalf("[에러] MQTT 연결 실패: %v\n", err)
	}
	defer mqttClient.Disconnect()

	// 5. 명령 응답 핸들러 등록 → MQTT response 토픽 발행
	responseTopic := topicBase + "/agent/response"
	cmdRouter.OnResponse(func(resp executor.Response) {
		data, err := json.Marshal(resp)
		if err != nil {
			log.Printf("[Executor] 응답 직렬화 실패: %v\n", err)
			return
		}
		if err := mqttClient.Publish(responseTopic, data); err != nil {
			log.Printf("[Executor] 응답 발행 실패: %v\n", err)
			return
		}
	})

	// 6. 명령 수신 토픽 구독
	commandTopic := topicBase + "/agent/command"
	if err := mqttClient.Subscribe(commandTopic, nil); err != nil {
		log.Fatalf("[에러] 토픽 구독 실패: %v\n", err)
	}

	// 7. 이상치 분석 엔진 초기화
	anomalyEngine = anomaly.NewEngine(anomaly.DefaultEngineConfig())

	// 알림 → MQTT 발행 핸들러 등록
	alertTopic := topicBase + "/anomaly/alert"
	anomalyEngine.OnAlert(func(alert anomaly.Alert) {
		if data, err := anomaly.AlertToJSON(alert); err == nil {
			if err := mqttClient.Publish(alertTopic, data); err != nil {
				log.Printf("[Anomaly] MQTT 발행 실패: %v\n", err)
			}
		}
	})

	// 8. 연결 완료 메시지 발행
	statusTopic := topicBase + "/agent/status"
	statusMsg, _ := json.Marshal(map[string]interface{}{
		"status":    "online",
		"version":   "1.0.0",
		"os":        getOS(),
		"arch":      getArch(),
		"timestamp": time.Now().Unix(),
	})
	if err := mqttClient.Publish(statusTopic, statusMsg); err != nil {
		log.Printf("[경고] 상태 메시지 발행 실패: %v\n", err)
	}

	// 9. 주기적 스캔 시작
	stopUSB := startPeriodicPublisher("USB", usbScanInterval, mqttClient, topicBase+"/scan/usb", publishUSBScan)
	stopHealth := startPeriodicPublisher("Health", healthScanInterval, mqttClient, topicBase+"/system/health", publishHealthScan)
	stopProcess := startPeriodicPublisher("Process", processScanInterval, mqttClient, topicBase+"/scan/process", publishProcessScan)
	stopNetwork := startPeriodicPublisher("Network", networkScanInterval, mqttClient, topicBase+"/scan/network", publishNetworkScan)

	// 10. 시그널 대기 (graceful shutdown)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	// 11. 정리
	stopUSB()
	stopHealth()
	stopProcess()
	stopNetwork()

	offlineMsg, _ := json.Marshal(map[string]interface{}{
		"status":    "offline",
		"timestamp": time.Now().Unix(),
	})
	_ = mqttClient.Publish(statusTopic, offlineMsg)

	fmt.Println("\n=== SocQuery Agent 종료 ===")
}

// ---------------------------------------------------------------------------
// 주기적 발행기
// ---------------------------------------------------------------------------

type publishFunc func(client *protocol.MQTTClient, topic string)

func startPeriodicPublisher(name string, interval time.Duration, client *protocol.MQTTClient, topic string, fn publishFunc) func() {
	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	go func() {
		fn(client, topic)
		for {
			select {
			case <-ticker.C:
				fn(client, topic)
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	return func() { close(done) }
}

// ---------------------------------------------------------------------------
// MQTT 발행 + Anomaly 피딩 함수들
// ---------------------------------------------------------------------------

func publishUSBScan(mqttClient *protocol.MQTTClient, topic string) {
	devices, err := scanner.ScanUSBDevices()
	if err != nil {
		log.Printf("[USB] 스캔 실패: %v\n", err)
		return
	}

	// anomaly 엔진에 피딩
	if anomalyEngine != nil {
		anomalyEngine.FeedUSB(devices)
	}

	payload := map[string]interface{}{
		"deviceCount": len(devices),
		"devices":     devices,
		"timestamp":   time.Now().Unix(),
	}

	if data, err := json.Marshal(payload); err == nil {
		if err := mqttClient.Publish(topic, data); err != nil {
			log.Printf("[USB] MQTT 발행 실패: %v\n", err)
			return
		}
	}
}

func publishHealthScan(mqttClient *protocol.MQTTClient, topic string) {
	info, err := scanner.ScanHealth()
	if err != nil {
		log.Printf("[Health] 스캔 실패: %v\n", err)
		return
	}

	// anomaly 엔진에 피딩
	if anomalyEngine != nil {
		anomalyEngine.FeedHealth(info)
	}

	payload := map[string]interface{}{
		"cpu":       info.CPU,
		"memory":    info.Memory,
		"disk":      info.Disk,
		"os":        info.OS,
		"timestamp": time.Now().Unix(),
	}

	if data, err := json.Marshal(payload); err == nil {
		if err := mqttClient.Publish(topic, data); err != nil {
			log.Printf("[Health] MQTT 발행 실패: %v\n", err)
			return
		}
	}
}

func publishProcessScan(mqttClient *protocol.MQTTClient, topic string) {
	result, err := scanner.ScanProcesses()
	if err != nil {
		log.Printf("[Process] 스캔 실패: %v\n", err)
		return
	}

	// anomaly 엔진에 피딩
	if anomalyEngine != nil {
		anomalyEngine.FeedProcesses(result)
	}

	payload := map[string]interface{}{
		"processCount": result.TotalCount,
		"topCpu":       result.TopCPU,
		"topMem":       result.TopMem,
		"timestamp":    time.Now().Unix(),
	}

	if data, err := json.Marshal(payload); err == nil {
		if err := mqttClient.Publish(topic, data); err != nil {
			log.Printf("[Process] MQTT 발행 실패: %v\n", err)
			return
		}
	}
}

func publishNetworkScan(mqttClient *protocol.MQTTClient, topic string) {
	result, err := scanner.ScanNetwork()
	if err != nil {
		log.Printf("[Network] 스캔 실패: %v\n", err)
		return
	}

	// anomaly 엔진에 피딩
	if anomalyEngine != nil {
		anomalyEngine.FeedNetwork(result)
	}

	payload := map[string]interface{}{
		"interfaces":  result.Interfaces,
		"connections": result.Connections,
		"dns":         result.DNS,
		"timestamp":   time.Now().Unix(),
	}

	if data, err := json.Marshal(payload); err == nil {
		if err := mqttClient.Publish(topic, data); err != nil {
			log.Printf("[Network] MQTT 발행 실패: %v\n", err)
			return
		}
	}
}

// ---------------------------------------------------------------------------
// 이상치 감지 테스트
// ---------------------------------------------------------------------------

func runDetectTest() {
	fmt.Println("=== 이상치 감지 테스트 (4축 분석) ===")
	fmt.Println()

	engine := anomaly.NewEngine(anomaly.DefaultEngineConfig())

	// 콘솔 핸들러 등록 (테스트용)
	var allAlerts []anomaly.Alert
	engine.OnAlert(func(alert anomaly.Alert) {
		allAlerts = append(allAlerts, alert)
	})

	// 1축: Health 스캔 → 리소스
	fmt.Println("[1축] 리소스 스캔 중...")
	health, err := scanner.ScanHealth()
	if err != nil {
		log.Printf("  Health 스캔 실패: %v\n", err)
	} else {
		engine.FeedHealth(health)
		fmt.Printf("  CPU: %.1f%% | Mem: %.1f%% | Disk: %.1f%%\n",
			health.CPU.UsagePercent, health.Memory.UsagePercent, health.Disk.UsagePercent)
	}

	// 2축: Process 스캔 → 프로세스
	fmt.Println("[2축] 프로세스 스캔 중...")
	procs, err := scanner.ScanProcesses()
	if err != nil {
		log.Printf("  Process 스캔 실패: %v\n", err)
	} else {
		engine.FeedProcesses(procs)
		fmt.Printf("  전체 %d개 프로세스\n", procs.TotalCount)
	}

	// 3축: Network 스캔 → 네트워크
	fmt.Println("[3축] 네트워크 스캔 중...")
	net, err := scanner.ScanNetwork()
	if err != nil {
		log.Printf("  Network 스캔 실패: %v\n", err)
	} else {
		engine.FeedNetwork(net)
		fmt.Printf("  DNS: %v (%dms)\n", net.DNS.Reachable, net.DNS.LatencyMs)
	}

	// 4축: USB 스캔 → 디바이스
	fmt.Println("[4축] USB 스캔 중...")
	devices, err := scanner.ScanUSBDevices()
	if err != nil {
		log.Printf("  USB 스캔 실패: %v\n", err)
	} else {
		engine.FeedUSB(devices)
		fmt.Printf("  %d개 디바이스\n", len(devices))
	}

	// 결과 출력
	fmt.Println()
	fmt.Println("=== 분석 결과 ===")
	fmt.Println(anomaly.FormatAlertSummary(allAlerts))

	// 임계치 정보 출력
	fmt.Println("--- 적용된 임계치 ---")
	cfg := anomaly.DefaultEngineConfig()
	fmt.Printf("  CPU:  경고 >%.0f%% (%d회 연속) | 심각 >%.0f%%\n",
		cfg.Resource.CPUWarningPct, cfg.Resource.CPUDurationSec, cfg.Resource.CPUCriticalPct)
	fmt.Printf("  MEM:  경고 >%.0f%% (%d회 연속) | 심각 >%.0f%%\n",
		cfg.Resource.MemWarningPct, cfg.Resource.MemDurationSec, cfg.Resource.MemCriticalPct)
	fmt.Printf("  Disk: 경고 >%.0f%% | 심각 >%.0f%%\n",
		cfg.Resource.DiskWarningPct, cfg.Resource.DiskCriticalPct)
	fmt.Printf("  DNS:  경고 %d회 연속실패 | 심각 %d회\n",
		cfg.Network.DNSFailWarningCount, cfg.Network.DNSFailCriticalCount)
	fmt.Printf("  MQTT: warning %d/%d min | critical %d/%d min\n",
		cfg.Network.MQTTDisconnectWarning, cfg.Network.MQTTDisconnectWindowMin,
		cfg.Network.MQTTDisconnectCritical, cfg.Network.MQTTDisconnectWindowMin)
	fmt.Printf("  CPU Hog:  >%.0f%% %d회 연속\n",
		cfg.Process.CPUHogThresholdPct, cfg.Process.HogDurationCount)
	fmt.Printf("  MEM Hog:  >%.0f%% %d회 연속\n",
		cfg.Process.MemHogThresholdPct, cfg.Process.HogDurationCount)
}

// ---------------------------------------------------------------------------
// HTTP 프록시 테스트
// ---------------------------------------------------------------------------

func runHTTPTest(url string) {
	fmt.Println("=== HTTP 프록시 테스트 ===")
	fmt.Printf("URL: %s\n\n", url)

	// Router를 통한 명령 실행 시뮬레이션
	router := executor.NewRouter()

	var result executor.Response
	router.OnResponse(func(resp executor.Response) {
		result = resp
	})

	// http_request 명령 생성
	cmdPayload, _ := json.Marshal(executor.Command{
		CommandID: "test-http-001",
		Action:    executor.ActionHTTPRequest,
		Params: map[string]interface{}{
			"method":  "GET",
			"url":     url,
			"timeout": 10,
		},
		Timeout: 15,
	})

	router.HandleCommand(cmdPayload)

	// 비동기 실행 대기
	time.Sleep(2 * time.Second)

	// 결과 출력
	if result.CommandID == "" {
		// 아직 실행 중일 수 있으므로 추가 대기
		time.Sleep(8 * time.Second)
	}

	fmt.Printf("[결과] commandId: %s\n", result.CommandID)
	fmt.Printf("[결과] exitCode:  %d\n", result.ExitCode)
	fmt.Printf("[결과] duration:  %dms\n", result.DurationMs)
	if result.Error != "" {
		fmt.Printf("[결과] error:     %s\n", result.Error)
	}

	if result.Data != nil {
		dataJSON, _ := json.MarshalIndent(result.Data, "", "  ")
		// 너무 길면 잘라서 표시
		output := string(dataJSON)
		if len(output) > 3000 {
			output = output[:3000] + "\n  ... (truncated)"
		}
		fmt.Printf("[결과] data:\n%s\n", output)
	}
}

// ---------------------------------------------------------------------------
// 테스트용 1회 스캔 함수들
// ---------------------------------------------------------------------------

func runAllScanOnce() {
	fmt.Println("=== 전체 스캔 ===")
	fmt.Println()
	runHealthScanOnce()
	fmt.Println()
	runProcessScanOnce()
	fmt.Println()
	runNetworkScanOnce()
	fmt.Println()
	runUSBScanOnce()
}

func runUSBScanOnce() {
	fmt.Println("=== USB 디바이스 스캔 ===")

	devices, err := scanner.ScanUSBDevices()
	if err != nil {
		log.Fatalf("[에러] USB 스캔 실패: %v\n", err)
	}

	if len(devices) == 0 {
		fmt.Println("연결된 USB 디바이스가 없습니다.")
		return
	}

	fmt.Printf("총 %d개 USB 디바이스 발견:\n\n", len(devices))
	for i, d := range devices {
		fmt.Printf("[%d] %s %s\n", i+1, d.Vendor, d.Product)
		fmt.Printf("    VendorID:  %s\n", d.VendorID)
		fmt.Printf("    ProductID: %s\n", d.ProductID)
		if d.Serial != "" {
			fmt.Printf("    Serial:    %s\n", d.Serial)
		}
		if d.Class != "" {
			fmt.Printf("    Class:     %s\n", d.Class)
		}
		if d.BusNumber != "" {
			fmt.Printf("    Bus:       %s\n", d.BusNumber)
		}
		fmt.Println()
	}
}

func runHealthScanOnce() {
	fmt.Println("=== OS Health 스캔 ===")

	info, err := scanner.ScanHealth()
	if err != nil {
		log.Fatalf("[에러] Health 스캔 실패: %v\n", err)
	}

	fmt.Printf("\n[CPU]\n")
	fmt.Printf("  사용률:    %.2f%%\n", info.CPU.UsagePercent)
	fmt.Printf("  코어 수:   %d\n", info.CPU.Cores)
	fmt.Printf("  로드 평균: %.2f, %.2f, %.2f\n",
		info.CPU.LoadAvg[0], info.CPU.LoadAvg[1], info.CPU.LoadAvg[2])

	fmt.Printf("\n[메모리]\n")
	fmt.Printf("  전체:   %d MB\n", info.Memory.TotalMB)
	fmt.Printf("  사용:   %d MB\n", info.Memory.UsedMB)
	fmt.Printf("  가용:   %d MB\n", info.Memory.AvailableMB)
	fmt.Printf("  사용률: %.2f%%\n", info.Memory.UsagePercent)

	fmt.Printf("\n[디스크]\n")
	fmt.Printf("  전체:   %.2f GB\n", info.Disk.TotalGB)
	fmt.Printf("  사용:   %.2f GB\n", info.Disk.UsedGB)
	fmt.Printf("  여유:   %.2f GB\n", info.Disk.FreeGB)
	fmt.Printf("  사용률: %.2f%%\n", info.Disk.UsagePercent)

	fmt.Printf("\n[OS]\n")
	fmt.Printf("  플랫폼:  %s\n", info.OS.Platform)
	fmt.Printf("  호스트:  %s\n", info.OS.Hostname)
	fmt.Printf("  커널:    %s\n", info.OS.KernelVersion)
	fmt.Printf("  업타임:  %s (%d초)\n", formatUptime(info.OS.Uptime), info.OS.Uptime)
}

func runProcessScanOnce() {
	fmt.Println("=== 프로세스 스캔 ===")

	result, err := scanner.ScanProcesses()
	if err != nil {
		log.Fatalf("[에러] Process 스캔 실패: %v\n", err)
	}

	fmt.Printf("전체 프로세스: %d개\n", result.TotalCount)

	fmt.Printf("\n[CPU Top %d]\n", len(result.TopCPU))
	fmt.Printf("  %-8s %-20s %-10s %8s %8s\n", "PID", "이름", "사용자", "CPU%", "MEM(MB)")
	fmt.Printf("  %-8s %-20s %-10s %8s %8s\n", "---", "----", "------", "----", "-------")
	for _, p := range result.TopCPU {
		name := truncate(p.Name, 20)
		user := truncate(p.User, 10)
		fmt.Printf("  %-8d %-20s %-10s %7.1f%% %7.1f\n", p.PID, name, user, p.CPUPct, p.MemMB)
	}

	fmt.Printf("\n[Memory Top %d]\n", len(result.TopMem))
	fmt.Printf("  %-8s %-20s %-10s %8s %8s\n", "PID", "이름", "사용자", "MEM%", "MEM(MB)")
	fmt.Printf("  %-8s %-20s %-10s %8s %8s\n", "---", "----", "------", "----", "-------")
	for _, p := range result.TopMem {
		name := truncate(p.Name, 20)
		user := truncate(p.User, 10)
		fmt.Printf("  %-8d %-20s %-10s %7.1f%% %7.1f\n", p.PID, name, user, p.MemPct, p.MemMB)
	}
}

func runNetworkScanOnce() {
	fmt.Println("=== 네트워크 스캔 ===")

	result, err := scanner.ScanNetwork()
	if err != nil {
		log.Fatalf("[에러] Network 스캔 실패: %v\n", err)
	}

	fmt.Printf("\n[인터페이스] %d개\n", len(result.Interfaces))
	for _, iface := range result.Interfaces {
		fmt.Printf("  %s (%s)\n", iface.Name, iface.Status)
		if iface.MAC != "" {
			fmt.Printf("    MAC:  %s\n", iface.MAC)
		}
		for _, ip := range iface.IPv4 {
			fmt.Printf("    IPv4: %s\n", ip)
		}
		for _, ip := range iface.IPv6 {
			fmt.Printf("    IPv6: %s\n", ip)
		}
	}

	fmt.Printf("\n[연결 통계]\n")
	fmt.Printf("  전체:        %d\n", result.Connections.Total)
	fmt.Printf("  ESTABLISHED: %d\n", result.Connections.Established)
	fmt.Printf("  LISTENING:   %d\n", result.Connections.Listening)
	fmt.Printf("  TIME_WAIT:   %d\n", result.Connections.TimeWait)
	fmt.Printf("  CLOSE_WAIT:  %d\n", result.Connections.CloseWait)

	fmt.Printf("\n[DNS]\n")
	fmt.Printf("  서버:     %s\n", result.DNS.Server)
	fmt.Printf("  접근가능: %v\n", result.DNS.Reachable)
	fmt.Printf("  응답시간: %dms\n", result.DNS.LatencyMs)
}

// ---------------------------------------------------------------------------
// 유틸리티
// ---------------------------------------------------------------------------

func getOS() string {
	return runtime.GOOS
}

func getArch() string {
	return runtime.GOARCH
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
