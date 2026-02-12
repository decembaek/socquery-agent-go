package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	// 기본 서버 등록 엔드포인트 (SOC_REGISTER_URL 미설정 시 사용)
	defaultRegisterURL = "https://socquery.com/api/v1/agents/register"
)

func getRegisterURL() string {
	if u := os.Getenv("SOC_REGISTER_URL"); u != "" {
		return u
	}
	return defaultRegisterURL
}

// AgentConfig 는 AWS IoT 연결에 필요한 설정을 담고 있습니다.
type AgentConfig struct {
	ThingName      string `json:"thingName"`
	TenantID       string `json:"tenant_id"`
	CertificatePem string `json:"certificatePem"`
	PrivateKey     string `json:"privateKey"`
	IotEndpoint    string `json:"iotEndpoint"`
	Region         string `json:"region"`
}

// EncryptedStore 는 암호화된 상태로 디스크에 저장되는 구조체입니다.
type EncryptedStore struct {
	Data string `json:"data"` // AES-GCM 암호화된 base64 문자열
}

// RegisterRequest 는 서버에 등록 요청을 보낼 때 사용하는 구조체입니다.
type RegisterRequest struct {
	Token     string `json:"token"`
	MachineID string `json:"machineId"`
}

// getStoragePath 는 OS별 표준 설정 디렉토리에 암호화 파일 경로를 반환합니다.
func getStoragePath() string {
	configDir, _ := os.UserConfigDir()
	path := filepath.Join(configDir, "socquery-agent", "auth.enc")
	os.MkdirAll(filepath.Dir(path), 0700)
	return path
}

// ClearStoredConfig 는 저장된 인증 정보(auth.enc)를 삭제합니다.
// 다음 실행 시 새 등록 토큰으로 다시 등록할 수 있습니다.
func ClearStoredConfig() error {
	path := getStoragePath()
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("설정 파일 삭제 실패: %w", err)
	}
	return nil
}

// GetStoragePath 는 저장 경로를 반환합니다 (초기화 시 경로 안내용).
func GetStoragePath() string {
	configDir, _ := os.UserConfigDir()
	return filepath.Join(configDir, "socquery-agent", "auth.enc")
}

// saveConfig 는 AgentConfig를 machineID로 암호화하여 디스크에 저장합니다.
func saveConfig(cfg *AgentConfig, machineID string) error {
	// JSON 직렬화
	jsonData, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("설정 직렬화 실패: %w", err)
	}

	// machineID로 암호화
	encrypted, err := Encrypt(string(jsonData), machineID)
	if err != nil {
		return fmt.Errorf("암호화 실패: %w", err)
	}

	// 암호화된 데이터를 파일로 저장
	store := EncryptedStore{Data: encrypted}
	storeData, err := json.Marshal(store)
	if err != nil {
		return fmt.Errorf("저장 구조체 직렬화 실패: %w", err)
	}

	path := getStoragePath()
	if err := os.WriteFile(path, storeData, 0600); err != nil {
		return fmt.Errorf("파일 저장 실패: %w", err)
	}

	return nil
}

// loadConfig 는 디스크에서 암호화된 설정을 읽어 machineID로 복호화합니다.
func loadConfig(machineID string) (*AgentConfig, error) {
	path := getStoragePath()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("설정 파일 읽기 실패: %w", err)
	}

	var store EncryptedStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("저장 구조체 파싱 실패: %w", err)
	}

	// machineID로 복호화
	decrypted, err := Decrypt(store.Data, machineID)
	if err != nil {
		return nil, fmt.Errorf("복호화 실패: %w", err)
	}

	var cfg AgentConfig
	if err := json.Unmarshal([]byte(decrypted), &cfg); err != nil {
		return nil, fmt.Errorf("설정 파싱 실패: %w", err)
	}

	return &cfg, nil
}

// registerWithServer 는 서버에 토큰과 machineID를 전송하여 인증 정보를 발급받습니다.
func registerWithServer(token, machineID string) (*AgentConfig, error) {
	reqBody := RegisterRequest{
		Token:     token,
		MachineID: machineID,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("요청 직렬화 실패: %w", err)
	}

	resp, err := http.Post(getRegisterURL(), "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("서버 등록 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("서버 등록 실패 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var cfg AgentConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("응답 파싱 실패: %w", err)
	}

	return &cfg, nil
}

// LoadOrRegister 는 기존 설정을 로드하거나, 없으면 서버에 등록하여 새 설정을 받아옵니다.
// token이 빈 문자열이면 기존 설정 로드만 시도합니다.
func LoadOrRegister(token string) (*AgentConfig, error) {
	// 1. machineID 가져오기
	machineID, err := GetMachineID()
	if err != nil {
		return nil, fmt.Errorf("머신 ID 조회 실패: %w", err)
	}

	// 2. 기존 암호화된 설정 파일 로드 시도
	if cfg, err := loadConfig(machineID); err == nil {
		fmt.Println("[설정] 기존 인증 정보 로드 성공")
		return cfg, nil
	}

	// 3. 기존 설정이 없고 토큰도 없으면 에러
	if token == "" {
		return nil, fmt.Errorf("인증 정보가 없습니다. --token 플래그로 등록 토큰을 제공하세요")
	}

	// 4. 서버에 등록 요청
	fmt.Println("[설정] 서버에 장비 등록 요청 중...")
	cfg, err := registerWithServer(token, machineID)
	if err != nil {
		return nil, fmt.Errorf("장비 등록 실패: %w", err)
	}

	// 5. 받은 설정을 암호화하여 디스크에 저장
	if err := saveConfig(cfg, machineID); err != nil {
		return nil, fmt.Errorf("설정 저장 실패: %w", err)
	}

	fmt.Println("[설정] 장비 등록 및 인증 정보 저장 완료")
	return cfg, nil
}
