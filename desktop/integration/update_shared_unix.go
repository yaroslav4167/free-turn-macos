//go:build linux || darwin

package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var updateAck *os.File

type UpdateResult struct {
	Message    string `json:"message"`
	BackupPath string `json:"backupPath"`
}

func updateResultPath() (string, error) {
	config, err := os.UserConfigDir()
	return filepath.Join(config, "FreeTurnDesktop", "update-result.json"), err
}

func LoadUpdateResult() UpdateResult {
	var result UpdateResult
	filename, err := updateResultPath()
	if err != nil {
		return result
	}
	data, err := os.ReadFile(filename)
	if err == nil && len(data) < 16384 {
		_ = json.Unmarshal(data, &result)
	}
	return result
}

func saveUpdateResult(result UpdateResult) error {
	filename, err := updateResultPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(filename), ".update-result-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filename)
}

func AcknowledgeUpdate() {
	if updateAck != nil {
		_, _ = updateAck.Write([]byte{1})
		_ = updateAck.Close()
		updateAck = nil
	}
}

func restartFallback(executable, reason string) error {
	cmd := exec.Command(executable)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s; не удалось перезапустить прежний клиент: %w", reason, err)
	}
	return cmd.Process.Release()
}

func restartUpdated(executable, stage string) error {
	read, write, err := os.Pipe()
	if err != nil {
		return err
	}
	defer read.Close()
	defer write.Close()
	log, err := os.OpenFile(filepath.Join(stage, "restart.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(executable, "--updated")
	cmd.ExtraFiles = []*os.File{write}
	cmd.Env = append(os.Environ(), "FREETURN_UPDATE_ACK_FD=3")
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		return err
	}
	write.Close()
	_ = read.SetReadDeadline(time.Now().Add(45 * time.Second))
	marker := []byte{0}
	if _, err := io.ReadFull(read, marker); err != nil || marker[0] != 1 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return errors.New("новый интерфейс не подтвердил запуск за 45 секунд")
	}
	return cmd.Process.Release()
}
