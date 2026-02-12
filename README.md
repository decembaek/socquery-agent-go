# SocQuery Client Agent

SaaS 웹 콘솔에서 장비를 등록하고, AWS IoT Core와 MQTT로 통신하는 경량 에이전트입니다.
헬스 모니터링, 이상치 감지, 원격 명령 실행(HTTP 프록시 포함)을 지원합니다.

---

## 프로젝트 구조

```
client-agent/
├── cmd/
│   └── agent/
│       └── main.go                  # 진입점 (CLI → 설정 → MQTT → 스캔 → 이상치 감지)
├── internal/
│   ├── config/
│   │   ├── load_machine_id.go       # OS별 고유 머신 ID 조회
│   │   ├── crypto.go                # AES-256-GCM 암호화/복호화
│   │   └── load_set.go              # 설정 로드/등록 + 암호화 저장
│   ├── protocol/
│   │   └── aws_mqtt_client.go       # AWS IoT Core MQTT 클라이언트 (mTLS + SNI)
│   ├── scanner/
│   │   ├── usb.go / usb_{os}.go     # USB 디바이스 스캔 (Linux/Windows/macOS)
│   │   ├── health.go / health_{os}.go # OS 헬스 스캔 (CPU/메모리/디스크)
│   │   ├── process.go / process_{os}.go # 프로세스 스캔 (Top CPU/MEM)
│   │   └── network.go / network_{os}.go # 네트워크 스캔 (인터페이스/연결/DNS)
│   ├── anomaly/
│   │   ├── engine.go                # 4축 이상치 감지 엔진 (중앙 관리)
│   │   ├── alert.go                 # Alert 타입 + JSON 변환 + 포맷팅
│   │   ├── resource.go              # 1축: CPU/메모리/디스크 임계치 감지
│   │   ├── process.go               # 2축: 프로세스 CPU/MEM Hog 감지
│   │   ├── network.go               # 3축: DNS 실패/MQTT 끊김 빈도 감지
│   │   └── usb.go                   # 4축: USB 디바이스 추가/제거 감지
│   └── executor/
│       ├── command.go               # 명령/응답 구조체 + Action 상수
│       ├── router.go                # 명령 라우터 (MQTT → 핸들러 디스패치)
│       └── httpclient.go            # HTTP 프록시 클라이언트
├── TOPIC.md                         # MQTT 토픽 설계 문서
├── build.sh                         # 크로스 플랫폼 빌드 스크립트
├── go.mod
└── go.sum
```

---

## 전체 동작 플로우

```
┌─────────────────────────────────────────────────────────────────┐
│  SaaS 웹 콘솔                                                    │
│  "장비 등록" → 등록 토큰 발급 → CLI 커맨드 제공                      │
│                                                                  │
│  예) SOC_REGISTER_URL=http://localhost:8787/api/v1/agents/register \
│      soc --token "등록토큰"                                       │
└──────────────────────────┬──────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  1. 머신 ID 조회                                                  │
│     machineid.ProtectedID("socquery-agent")                      │
│     → OS별 고유 하드웨어 ID를 해시하여 반환                           │
└──────────────────────────┬───────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  2. 기존 설정 파일 확인                                             │
│     경로: $USER_CONFIG_DIR/socquery-agent/auth.enc                │
│     ├─ 있음 → machineID로 복호화 → AgentConfig 로드 → 5단계로       │
│     └─ 없음 → 3단계로                                             │
└──────────────────────────┬───────────────────────────────────────┘
                           │ (파일 없음)
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  3. 서버 등록 요청                                                 │
│     POST https://socquery.com/api/v1/agents/register             │
│     Body: { "token": "...", "machineId": "..." }                 │
│     (로컬: SOC_REGISTER_URL 환경변수로 오버라이드 가능)               │
│                                                                   │
│     응답: thingName, tenant_id, certificatePem, privateKey,      │
│           iotEndpoint, region                                     │
└──────────────────────────┬───────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  4. 암호화 저장                                                    │
│     AgentConfig → JSON → AES-256-GCM 암호화 (키: machineID 파생)  │
│     → auth.enc 파일로 저장 (권한 0600)                              │
└──────────────────────────┬───────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  5. AWS IoT Core MQTT 연결                                        │
│     인증서 PEM → 메모리에서 TLS 설정 생성 (SNI 포함)                 │
│     tls://{iotEndpoint}:8883 연결 (mTLS)                          │
│                                                                   │
│     구독: socquery/{tenant_id}/{thingName}/agent/command          │
│     발행: socquery/{tenant_id}/{thingName}/agent/status           │
│           → {"status":"online","version":"1.0.0","os":"..."}     │
└──────────────────────────┬───────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  6. 주기적 스캔 + MQTT 발행 + 이상치 감지                            │
│                                                                   │
│  ┌─ USB 스캔 (60초) ──→ .../scan/usb ──→ anomaly 엔진 피딩       │
│  ├─ Health 스캔 (30초) → .../system/health → anomaly 엔진 피딩    │
│  ├─ Process 스캔 (60초) → .../scan/process → anomaly 엔진 피딩    │
│  ├─ Network 스캔 (60초) → .../scan/network → anomaly 엔진 피딩    │
│  └─ 이상치 알림 ─────────→ .../anomaly/alert (임계치 초과 시)      │
│                                                                   │
│  명령 수신 (agent/command) → Executor Router → 응답 (agent/response)│
└──────────────────────────┬───────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  7. 종료 (SIGINT/SIGTERM)                                         │
│     → {"status":"offline"} 발행 → 스캔 중지 → MQTT 연결 종료       │
└──────────────────────────────────────────────────────────────────┘
```

---

## MQTT 토픽 구조

```
socquery/{tenant_id}/{thingName}/
├── agent/
│   ├── status          → Agent → Cloud   연결 상태 (online/offline)
│   ├── command         ← Cloud → Agent   명령 전달
│   └── response        → Agent → Cloud   명령 실행 결과
├── scan/
│   ├── usb             → Agent → Cloud   USB 디바이스 목록
│   ├── process         → Agent → Cloud   프로세스 목록
│   └── network         → Agent → Cloud   네트워크 상태
├── system/
│   └── health          → Agent → Cloud   OS 헬스 (CPU/메모리/디스크)
└── anomaly/
    └── alert           → Agent → Cloud   이상치 감지 알림
```

상세 토픽 설계는 [TOPIC.md](./TOPIC.md) 참고.

---

## MQTT 발행 주기

| 스캔 종류 | 주기           | 토픽                | 페이로드 요약                    |
| --------- | -------------- | ------------------- | -------------------------------- |
| Health    | 30초           | `.../system/health` | CPU%, 메모리%, 디스크%, OS 정보  |
| USB       | 60초           | `.../scan/usb`      | 디바이스 수, VID/PID/Serial 목록 |
| Process   | 60초           | `.../scan/process`  | 프로세스 수, Top CPU/MEM 목록    |
| Network   | 60초           | `.../scan/network`  | 인터페이스, 연결 통계, DNS 상태  |
| Status    | 시작/종료 시   | `.../agent/status`  | online/offline + OS/arch/version |
| Anomaly   | 임계치 초과 시 | `.../anomaly/alert` | severity, axis, message, details |

---

## 원격 명령 (Executor)

백엔드가 `agent/command` 토픽으로 명령을 보내면, 에이전트가 실행 후 `agent/response`로 결과를 반환합니다.

### 지원 Action

| Action         | 설명                              |
| -------------- | --------------------------------- |
| `ping`         | 연결 확인 (즉시 응답)             |
| `scan_usb`     | USB 디바이스 즉시 스캔            |
| `scan_health`  | OS 헬스 즉시 스캔                 |
| `scan_process` | 프로세스 즉시 스캔                |
| `scan_network` | 네트워크 즉시 스캔                |
| `scan_all`     | 전체 스캔                         |
| `http_request` | HTTP 프록시 요청 (내부/외부 대리) |

### HTTP 프록시 명령 예시

```json
{
  "commandId": "cmd-001",
  "action": "http_request",
  "params": {
    "method": "GET",
    "url": "http://localhost:3000/api/health",
    "headers": { "X-Custom": "value" },
    "timeout": 10
  },
  "timeout": 15
}
```

---

## 이상치 감지 (4축)

| 축          | 감지 대상                                             | 임계치 (기본값) |
| ----------- | ----------------------------------------------------- | --------------- |
| 1. 리소스   | CPU > 90% (30초 연속), 메모리 > 95%, 디스크 > 90%/95% | 경고/심각 2단계 |
| 2. 프로세스 | CPU/MEM Hog (단일 프로세스 > 80%, 3회 연속)           | 경고            |
| 3. 네트워크 | DNS 연속 실패 (3회/5회), MQTT 끊김 빈도               | 경고/심각 2단계 |
| 4. USB      | 디바이스 추가/제거 감지 (VID/PID/Serial 기반)         | 즉시 알림       |

---

## 모듈 상세

### `internal/config/` — 설정 관리

| 파일                 | 역할                                                        |
| -------------------- | ----------------------------------------------------------- |
| `load_machine_id.go` | `machineid.ProtectedID("socquery-agent")` → OS별 고유 ID    |
| `crypto.go`          | AES-256-GCM 암호화/복호화 (SHA-256 키 파생)                 |
| `load_set.go`        | 서버 등록, 설정 로드/저장, `SOC_REGISTER_URL` 환경변수 지원 |

### `internal/protocol/` — MQTT 통신

| 파일                 | 역할                                                    |
| -------------------- | ------------------------------------------------------- |
| `aws_mqtt_client.go` | AWS IoT Core mTLS 연결, SNI 설정, Amazon Root CA 1 내장 |

**연결 설정:**

| 항목        | 값                                          |
| ----------- | ------------------------------------------- |
| 프로토콜    | MQTT over TLS (포트 8883)                   |
| 인증        | mTLS (클라이언트 인증서 + Amazon Root CA 1) |
| SNI         | IoT ATS 엔드포인트 호스트명                 |
| QoS         | 1 (At least once)                           |
| KeepAlive   | 30초                                        |
| 자동 재연결 | 활성화 (최대 1분 간격)                      |

### `internal/scanner/` — 시스템 스캔

| 파일                             | 역할                               | 플랫폼                                                     |
| -------------------------------- | ---------------------------------- | ---------------------------------------------------------- |
| `usb.go` + `usb_{os}.go`         | USB 디바이스 열거 (VID/PID/Serial) | Linux: `/sys/bus/usb`, Windows: PowerShell, macOS: `ioreg` |
| `health.go` + `health_{os}.go`   | CPU/메모리/디스크/OS 정보          | Linux: `/proc`, Windows: `wmic`, macOS: `sysctl`/`vm_stat` |
| `process.go` + `process_{os}.go` | 프로세스 목록 (Top CPU/MEM)        | Linux/macOS: `top`/`ps`, Windows: `tasklist`               |
| `network.go` + `network_{os}.go` | 인터페이스/연결/DNS 상태           | Linux/macOS: `netstat`, Windows: `netstat`                 |

### `internal/anomaly/` — 이상치 감지 엔진

| 파일          | 역할                                                       |
| ------------- | ---------------------------------------------------------- |
| `engine.go`   | 중앙 엔진 (스캔 데이터 수신 → 감지기 호출 → 알림 디스패치) |
| `resource.go` | CPU/메모리/디스크 임계치 + 연속 조건 감지                  |
| `process.go`  | CPU/MEM Hog 프로세스 감지                                  |
| `network.go`  | DNS 실패/MQTT 끊김 빈도 감지                               |
| `usb.go`      | USB 디바이스 추가/제거 감지                                |
| `alert.go`    | Alert 구조체, JSON 변환, 콘솔 포맷팅                       |

### `internal/executor/` — 원격 명령 실행

| 파일            | 역할                                                              |
| --------------- | ----------------------------------------------------------------- |
| `command.go`    | `Command`/`Response` 구조체, Action 상수 정의                     |
| `router.go`     | MQTT 명령 파싱 → Action별 핸들러 디스패치 → 응답 콜백             |
| `httpclient.go` | HTTP 프록시 (GET/POST/PUT/DELETE, 헤더, 타임아웃, 응답 크기 제한) |

---

## 사용법

### 빌드

```bash
# 현재 OS용 빌드
go build -o soc ./cmd/agent/

# 크로스 플랫폼 빌드
chmod +x build.sh && ./build.sh
```

### 최초 등록

```bash
# 프로덕션
soc --token "등록토큰"

# 로컬 개발 (백엔드가 localhost:8787에서 실행 중일 때)
SOC_REGISTER_URL=http://localhost:8787/api/v1/agents/register soc --token "등록토큰"
```

### 이후 실행 (토큰 불필요)

```bash
soc
```

### 테스트용 CLI 플래그

```bash
soc --scan-usb        # USB 1회 스캔
soc --scan-health     # OS 헬스 1회 스캔
soc --scan-process    # 프로세스 1회 스캔
soc --scan-network    # 네트워크 1회 스캔
soc --scan-all        # 전체 스캔
soc --detect-test     # 이상치 감지 테스트 (4축 분석)
soc --http-test "URL" # HTTP 프록시 테스트
```

### 실행 예시

```
=== SocQuery Agent 시작 ===
[설정] 기존 인증 정보 로드 성공
[정보] ThingName: socquery-agent-abc12345-xxxx-xxxx-xxxx-xxxxxxxxxxxx
[정보] TenantID:  8a0544cc-3a37-41e6-9f52-620a5b80efca
[정보] Region:    ap-northeast-2
[정보] Endpoint:  a21saeo8azfndu-ats.iot.ap-northeast-2.amazonaws.com
[MQTT] AWS IoT Core 연결 중...
[MQTT] AWS IoT Core 연결 성공 (ThingName: socquery-agent-abc12345-...)
[MQTT] 토픽 구독 완료: socquery/8a0544cc-.../socquery-agent-abc12345-.../agent/command
[정보] 명령 실행기 초기화 완료 (HTTP 프록시 포함)
[정보] 이상치 분석 엔진 초기화 완료 (4축 감지)
=== 에이전트 실행 중 (Ctrl+C로 종료) ===
[Health] CPU: 12.3% | Mem: 45.6% | Disk: 58.2%
[USB] 스캔 완료: 3개 디바이스
[Process] 스캔 완료: 287개 프로세스
[Network] 인터페이스: 4개 | 연결: 52개 | DNS: true
```

---

## 환경 변수

| 변수               | 설명                                  | 기본값                                        |
| ------------------ | ------------------------------------- | --------------------------------------------- |
| `SOC_REGISTER_URL` | 등록 API URL 오버라이드 (로컬 개발용) | `https://socquery.com/api/v1/agents/register` |

---

## 보안

| 항목           | 방식                                                      |
| -------------- | --------------------------------------------------------- |
| 인증 정보 저장 | AES-256-GCM 암호화 (machineID 기반 키 파생)               |
| 키 파생        | SHA-256(machineID + salt) → 해당 머신에서만 복호화 가능   |
| 파일 권한      | `0600` (소유자만 읽기/쓰기), 디렉토리 `0700`              |
| 인증서 처리    | 파일 미저장, PEM 문자열을 메모리에서 직접 TLS 설정에 사용 |
| 통신           | mTLS (상호 인증서 기반 TLS 1.2+, SNI 필수)                |

**설정 파일 저장 경로:**

| OS      | 경로                                                    |
| ------- | ------------------------------------------------------- |
| Windows | `%APPDATA%\socquery-agent\auth.enc`                     |
| macOS   | `~/Library/Application Support/socquery-agent/auth.enc` |
| Linux   | `~/.config/socquery-agent/auth.enc`                     |

---

## 의존성

| 패키지                                      | 용도                                   |
| ------------------------------------------- | -------------------------------------- |
| `github.com/denisbrodbeck/machineid`        | OS별 고유 머신 ID 조회                 |
| `github.com/eclipse/paho.mqtt.golang`       | MQTT 클라이언트                        |
| Go 표준 라이브러리 (`crypto/*`, `net/http`) | AES-GCM, TLS, SHA-256, HTTP 클라이언트 |

---

## 서버 API

### `POST /api/v1/agents/register`

에이전트가 최초 등록 시 호출합니다. JWT 불필요 (등록 토큰으로 인증).

**Request:**

```json
{
  "token": "웹 콘솔에서 발급받은 등록 토큰",
  "machineId": "OS별 고유 머신 ID (해시)"
}
```

**Response (200 OK):**

```json
{
  "success": true,
  "thingName": "socquery-agent-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "tenant_id": "account-uuid",
  "certificatePem": "-----BEGIN CERTIFICATE-----\n...",
  "privateKey": "-----BEGIN RSA PRIVATE KEY-----\n...",
  "iotEndpoint": "xxxxx-ats.iot.ap-northeast-2.amazonaws.com",
  "region": "ap-northeast-2"
}
```
