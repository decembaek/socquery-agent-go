package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPRequest 는 서버가 에이전트에게 대리 실행을 요청하는 HTTP 요청 구조체입니다.
type HTTPRequest struct {
	Method  string            `json:"method"`            // GET, POST, PUT, DELETE, PATCH
	URL     string            `json:"url"`               // 대상 URL (내부/외부 모두 가능)
	Headers map[string]string `json:"headers,omitempty"` // 요청 헤더
	Body    interface{}       `json:"body,omitempty"`    // 요청 바디 (JSON)
	Timeout int               `json:"timeout,omitempty"` // 요청 타임아웃 (초, 기본 30)
}

// HTTPResponse 는 에이전트가 대리 실행한 HTTP 응답 결과입니다.
type HTTPResponse struct {
	StatusCode int               `json:"statusCode"`         // HTTP 상태 코드
	Headers    map[string]string `json:"headers"`            // 응답 헤더 (주요 헤더만)
	Body       interface{}       `json:"body,omitempty"`     // 응답 바디 (JSON 파싱 시도)
	BodyRaw    string            `json:"bodyRaw,omitempty"`  // 응답 바디 (raw string, JSON 파싱 실패 시)
	BodySize   int               `json:"bodySize"`           // 응답 바디 크기 (bytes)
	DurationMs int64             `json:"durationMs"`         // HTTP 요청 소요 시간 (ms)
}

// 응답 바디 최대 크기 (1MB)
const maxResponseBodySize = 1 * 1024 * 1024

// ExecuteHTTPRequest 는 HTTPRequest를 실행하고 결과를 반환합니다.
func ExecuteHTTPRequest(ctx context.Context, req *HTTPRequest) (*HTTPResponse, error) {
	// 1. 입력 검증
	if req.URL == "" {
		return nil, fmt.Errorf("URL이 비어있습니다")
	}

	method := strings.ToUpper(req.Method)
	if method == "" {
		method = "GET"
	}

	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "DELETE": true,
		"PATCH": true, "HEAD": true, "OPTIONS": true,
	}
	if !validMethods[method] {
		return nil, fmt.Errorf("지원하지 않는 HTTP 메서드: %s", method)
	}

	// 2. 요청 바디 직렬화
	var bodyReader io.Reader
	if req.Body != nil {
		bodyBytes, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("요청 바디 직렬화 실패: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// 3. 타임아웃 설정
	timeout := 30 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}

	// 상위 context의 deadline이 더 짧으면 그대로 사용
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 4. HTTP 요청 생성
	httpReq, err := http.NewRequestWithContext(reqCtx, method, req.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("HTTP 요청 생성 실패: %w", err)
	}

	// 5. 헤더 설정
	httpReq.Header.Set("User-Agent", "SocQuery-Agent/1.0")
	if req.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	for key, value := range req.Headers {
		httpReq.Header.Set(key, value)
	}

	// 6. HTTP 요청 실행
	client := &http.Client{
		Timeout: timeout,
		// 리다이렉트 최대 10회 (기본값 유지)
	}

	start := time.Now()
	httpResp, err := client.Do(httpReq)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		return nil, fmt.Errorf("HTTP 요청 실행 실패 (%dms): %w", durationMs, err)
	}
	defer httpResp.Body.Close()

	// 7. 응답 바디 읽기 (크기 제한)
	limitedReader := io.LimitReader(httpResp.Body, maxResponseBodySize+1)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("응답 바디 읽기 실패: %w", err)
	}

	if len(bodyBytes) > maxResponseBodySize {
		bodyBytes = bodyBytes[:maxResponseBodySize]
	}

	// 8. 응답 헤더 수집 (주요 헤더만)
	respHeaders := make(map[string]string)
	importantHeaders := []string{
		"Content-Type", "Content-Length", "Server",
		"X-Request-Id", "X-Correlation-Id",
		"Cache-Control", "ETag", "Last-Modified",
		"Location", "Retry-After",
	}
	for _, h := range importantHeaders {
		if v := httpResp.Header.Get(h); v != "" {
			respHeaders[h] = v
		}
	}

	// 9. 바디 파싱 (JSON 시도)
	resp := &HTTPResponse{
		StatusCode: httpResp.StatusCode,
		Headers:    respHeaders,
		BodySize:   len(bodyBytes),
		DurationMs: durationMs,
	}

	// JSON 파싱 시도
	var jsonBody interface{}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &jsonBody); err == nil {
			resp.Body = jsonBody
		} else {
			// JSON이 아니면 raw string (최대 10KB)
			raw := string(bodyBytes)
			if len(raw) > 10240 {
				raw = raw[:10240] + "... (truncated)"
			}
			resp.BodyRaw = raw
		}
	}

	return resp, nil
}

// ParseHTTPRequest 는 Command의 params에서 HTTPRequest를 추출합니다.
func ParseHTTPRequest(params map[string]interface{}) (*HTTPRequest, error) {
	// params를 JSON으로 변환 후 다시 역직렬화
	data, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("params 직렬화 실패: %w", err)
	}

	var req HTTPRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("HTTPRequest 파싱 실패: %w", err)
	}

	return &req, nil
}
