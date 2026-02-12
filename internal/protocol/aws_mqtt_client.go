package protocol

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"time"

	"github.com/decembaek/socquery-client-agent/internal/config"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	// Amazon Root CA 1 (인라인 포함)
	amazonRootCA1 = `-----BEGIN CERTIFICATE-----
MIIDQTCCAimgAwIBAgITBmyfz5m/jAo54vB4ikPmljZbyjANBgkqhkiG9w0BAQsF
ADA5MQswCQYDVQQGEwJVUzEPMA0GA1UEChMGQW1hem9uMRkwFwYDVQQDExBBbWF6
b24gUm9vdCBDQSAxMB4XDTE1MDUyNjAwMDAwMFoXDTM4MDExNzAwMDAwMFowOTEL
MAkGA1UEBhMCVVMxDzANBgNVBAoTBkFtYXpvbjEZMBcGA1UEAxMQQW1hem9uIFJv
b3QgQ0EgMTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBALJ4gHHKeNXj
ca9HgFB0fW7Y14h29Jlo91ghYPl0hAEvrAIthtOgQ3pOsqTQNroBvo3bSMgHFzZM
9O6II8c+6zf1tRn4SWiw3te5djgdYZ6k/oI2peVKVuRF4fn9tBb6dNqcmzU5L/qw
IFAGbHrQgLKm+a/sRxmPUDgH3KKHOVj4utWp+UhnMJbulHheb4mjUcAwhmahRWa6
VOujw5H5SNz/0egwLX0tdHA114gk957EWW67c4cX8jJGKLhD+rcdqsq08p8kDi1L
93FcXmn/6pUCyziKrlA4b9v7LWIbxcceVOF34GfID5yHI9Y/QCB/IIDEgEw+OyQm
jgSubJrIqg0CAwEAAaNCMEAwDwYDVR0TAQH/BAUwAwEB/zAOBgNVHQ8BAf8EBAMC
AYYwHQYDVR0OBBYEFIQYzIU07LwMlJQuCFmcx7IQTgoIMA0GCSqGSIb3DQEBCwUA
A4IBAQCY8jdaQZChGsV2USggNiMOruYou6r4lK5IpDB/G/wkjUu0yKGX9rbxenDI
U5PMCCjjmCXPI6T53iHTfIUJrU6adTrCC2qJeHZERxhlbI1Bjjt/msv0tadQ1wUs
N+gDS63pYaACbvXy8MWy7Vu33PqUXHeeE6V/Uq2V8viTO96LXFvKWlJbYK8U90vv
o/ufQJVtMVT8QtPHRh8jrdkPSHCa2XV4cdFyQzR1bldZwgJcJmApzyMZFo6IQ6XU
5MsI+yMRQ+hDKXJioaldXgjUkK642M4UwtBV8ob2xJNDd2ZhwLnoQdeXeGADbkpy
rqXRfboQnoZsG4q5WTP468SQvvG5
-----END CERTIFICATE-----`

	mqttQoS        = 1
	keepAlive      = 30 * time.Second
	pingTimeout    = 10 * time.Second
	connectTimeout = 30 * time.Second
)

// MQTTClient 는 AWS IoT Core에 연결하는 MQTT 클라이언트를 래핑합니다.
type MQTTClient struct {
	Client mqtt.Client
	Config *config.AgentConfig
}

// MessageHandler 는 MQTT 메시지 핸들러 함수 타입입니다.
type MessageHandler func(topic string, payload []byte)

// NewMQTTClient 는 AgentConfig의 인증서로 AWS IoT Core에 TLS 연결하는 클라이언트를 생성합니다.
func NewMQTTClient(cfg *config.AgentConfig, onMessage MessageHandler) (*MQTTClient, error) {
	// 1. 인증서 PEM 문자열에서 TLS 설정 생성 (SNI = 엔드포인트 호스트명 필수)
	tlsConfig, err := newTLSConfig(cfg.CertificatePem, cfg.PrivateKey, cfg.IotEndpoint)
	if err != nil {
		return nil, fmt.Errorf("TLS 설정 생성 실패: %w", err)
	}

	// 2. MQTT 브로커 URL (mqtts:// = TLS 포트 8883)
	brokerURL := fmt.Sprintf("tls://%s:8883", cfg.IotEndpoint)

	// 3. MQTT 클라이언트 옵션 설정
	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID(cfg.ThingName)
	opts.SetTLSConfig(tlsConfig)
	opts.SetKeepAlive(keepAlive)
	opts.SetPingTimeout(pingTimeout)
	opts.SetConnectTimeout(connectTimeout)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(1 * time.Minute)
	opts.SetCleanSession(true)

	// 연결 핸들러
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		fmt.Printf("[MQTT] AWS IoT Core 연결 성공 (ThingName: %s)\n", cfg.ThingName)
	})

	// 연결 끊김 핸들러
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		fmt.Printf("[MQTT] 연결 끊김: %v (자동 재연결 시도 중...)\n", err)
	})

	// 재연결 핸들러
	opts.SetReconnectingHandler(func(c mqtt.Client, opts *mqtt.ClientOptions) {
		fmt.Println("[MQTT] 재연결 시도 중...")
	})

	// 기본 메시지 핸들러
	if onMessage != nil {
		opts.SetDefaultPublishHandler(func(c mqtt.Client, msg mqtt.Message) {
			onMessage(msg.Topic(), msg.Payload())
		})
	}

	client := mqtt.NewClient(opts)

	return &MQTTClient{
		Client: client,
		Config: cfg,
	}, nil
}

// Connect 는 AWS IoT Core에 연결합니다.
func (m *MQTTClient) Connect() error {
	fmt.Println("[MQTT] AWS IoT Core 연결 중...")
	token := m.Client.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("MQTT 연결 실패: %w", err)
	}
	return nil
}

// Subscribe 는 지정된 토픽을 구독합니다.
func (m *MQTTClient) Subscribe(topic string, handler MessageHandler) error {
	var mqttHandler mqtt.MessageHandler
	if handler != nil {
		mqttHandler = func(c mqtt.Client, msg mqtt.Message) {
			handler(msg.Topic(), msg.Payload())
		}
	}

	token := m.Client.Subscribe(topic, mqttQoS, mqttHandler)
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("토픽 구독 실패 (%s): %w", topic, err)
	}
	fmt.Printf("[MQTT] 토픽 구독 완료: %s\n", topic)
	return nil
}

// Publish 는 지정된 토픽에 메시지를 발행합니다.
func (m *MQTTClient) Publish(topic string, payload interface{}) error {
	token := m.Client.Publish(topic, mqttQoS, false, payload)
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("메시지 발행 실패 (%s): %w", topic, err)
	}
	return nil
}

// Disconnect 는 MQTT 연결을 종료합니다.
func (m *MQTTClient) Disconnect() {
	m.Client.Disconnect(250)
	fmt.Println("[MQTT] 연결 종료")
}

// IsConnected 는 현재 연결 상태를 반환합니다.
func (m *MQTTClient) IsConnected() bool {
	return m.Client.IsConnected()
}

// newTLSConfig 는 PEM 문자열로부터 TLS 설정을 생성합니다.
// serverName은 AWS IoT 엔드포인트 호스트명으로, SNI에 필요합니다.
func newTLSConfig(certPEM, keyPEM, serverName string) (*tls.Config, error) {
	// 클라이언트 인증서 + 개인키 로드
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("X509 키 페어 로드 실패: %w", err)
	}

	// Amazon Root CA 추가
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM([]byte(amazonRootCA1)) {
		return nil, fmt.Errorf("Amazon Root CA 추가 실패")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		MinVersion:   tls.VersionTLS12,
		ServerName:   serverName, // AWS IoT ATS는 SNI 필수
	}, nil
}
