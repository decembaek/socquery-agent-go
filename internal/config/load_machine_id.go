package config

import (
	"github.com/denisbrodbeck/machineid"
)

// GetMachineID 는 OS별 고유 머신 ID를 반환합니다.
// "socquery-agent" salt를 사용하여 해시된 ID를 생성합니다.
func GetMachineID() (string, error) {
	id, err := machineid.ProtectedID("socquery-agent")
	if err != nil {
		return "", err
	}
	return id, nil
}
