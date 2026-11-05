//go:build !linux && !darwin

package integration

import (
	"errors"
	"os"
)

type UpdateResult struct {
	Message    string
	BackupPath string
}

func LoadUpdateResult() UpdateResult { return UpdateResult{} }
func StartUpdateWorker(string) (*os.File, error) {
	return nil, errors.New("установка обновлений поддерживается только на Linux")
}
func HandleUpdateProcess() bool { return false }
func AcknowledgeUpdate()        {}
