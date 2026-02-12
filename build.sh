#!/bin/bash

# Windows용 (exe 파일 생성)
GOOS=windows GOARCH=amd64 go build -o build/soc.exe ./cmd/agent/

# Linux용
GOOS=linux GOARCH=amd64 go build -o build/soc-linux ./cmd/agent/

# macOS용 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o build/soc-darwin ./cmd/agent/

echo "빌드 완료: build/ 디렉토리 확인"
