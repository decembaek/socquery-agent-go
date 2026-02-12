package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// deriveKey 는 machineID 문자열로부터 AES-256용 32바이트 키를 파생합니다.
func deriveKey(machineID string) []byte {
	hash := sha256.Sum256([]byte(machineID + ":socquery-agent-secret"))
	return hash[:]
}

// Encrypt 는 평문을 AES-256-GCM으로 암호화하고 base64 문자열로 반환합니다.
func Encrypt(plaintext string, machineID string) (string, error) {
	key := deriveKey(machineID)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("AES 블록 생성 실패: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("GCM 생성 실패: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce 생성 실패: %w", err)
	}

	// nonce + ciphertext 결합
	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 는 base64로 인코딩된 암호문을 AES-256-GCM으로 복호화합니다.
func Decrypt(encoded string, machineID string) (string, error) {
	key := deriveKey(machineID)

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 디코딩 실패: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("AES 블록 생성 실패: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("GCM 생성 실패: %w", err)
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("암호문이 너무 짧습니다")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("복호화 실패 (machineID 불일치 가능): %w", err)
	}

	return string(plaintext), nil
}
