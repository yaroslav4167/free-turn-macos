//go:build linux

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/samosvalishe/free-turn-proxy/desktop/updates"
)

type updateJob struct {
	Parent   int    `json:"parent"`
	Fallback string `json:"fallback"`
}

// StartUpdateWorker keeps the old GUI alive until the worker reports readiness.
// Closing the returned pipe during shutdown permits installation after GUI exit.
func StartUpdateWorker(stage string) (*os.File, error) {
	if os.Geteuid() == 0 {
		return nil, errors.New("обновление клиента нельзя запускать от root")
	}
	target, err := InstalledPath()
	if err != nil {
		return nil, err
	}
	if err := updates.ValidatePrepared(stage, filepath.Dir(target), runtime.GOARCH); err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(updateJob{Parent: os.Getpid(), Fallback: executable})
	if err := os.WriteFile(filepath.Join(stage, "worker.json"), data, 0o600); err != nil {
		return nil, err
	}
	gateRead, gateWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer gateRead.Close()
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		gateWrite.Close()
		return nil, err
	}
	defer readyRead.Close()
	defer readyWrite.Close()
	log, err := os.OpenFile(filepath.Join(stage, "worker.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		gateWrite.Close()
		return nil, err
	}
	defer log.Close()
	cmd := exec.Command(executable, "--apply-update", stage)
	cmd.ExtraFiles = []*os.File{gateRead, readyWrite}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		gateWrite.Close()
		return nil, err
	}
	readyWrite.Close()
	_ = readyRead.SetReadDeadline(time.Now().Add(15 * time.Second))
	marker := []byte{0}
	if _, err := io.ReadFull(readyRead, marker); err != nil || marker[0] != 1 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		gateWrite.Close()
		return nil, errors.New("не удалось подготовить процесс установки; клиент не закрыт")
	}
	_ = cmd.Process.Release()
	return gateWrite, nil
}

// HandleUpdateProcess is called before Wails startup. The worker never installs
// root-owned files, executes a shell or touches server profiles.
func HandleUpdateProcess() bool {
	if len(os.Args) == 2 && os.Args[1] == "--update-protocol" {
		fmt.Println(updates.InstallProtocolMarker)
		return true
	}
	if len(os.Args) == 3 && os.Args[1] == "--apply-update" {
		if err := runUpdateWorker(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return true
	}
	if len(os.Args) == 2 && os.Args[1] == "--updated" && os.Getenv("FREETURN_UPDATE_ACK_FD") == "3" {
		updateAck = os.NewFile(3, "update-ack")
	}
	_ = os.Unsetenv("FREETURN_UPDATE_ACK_FD")
	return false
}

func runUpdateWorker(stage string) error {
	if os.Geteuid() == 0 {
		return errors.New("updater cannot run as root")
	}
	target, err := InstalledPath()
	if err != nil {
		return err
	}
	targetDir := filepath.Dir(target)
	if err := updates.ValidatePrepared(stage, targetDir, runtime.GOARCH); err != nil {
		return err
	}
	var job updateJob
	data, err := os.ReadFile(filepath.Join(stage, "worker.json"))
	if err != nil {
		return err
	}
	if len(data) > 16384 {
		return errors.New("invalid update job")
	}
	if err := json.Unmarshal(data, &job); err != nil {
		return err
	}
	if job.Parent != os.Getppid() || !filepath.IsAbs(job.Fallback) {
		return errors.New("invalid update parent")
	}
	gate := os.NewFile(3, "update-gate")
	ready := os.NewFile(4, "update-ready")
	if gate == nil || ready == nil {
		return errors.New("missing update pipes")
	}
	defer gate.Close()
	if _, err := ready.Write([]byte{1}); err != nil {
		ready.Close()
		return err
	}
	ready.Close()
	_ = gate.SetReadDeadline(time.Now().Add(2 * time.Minute))
	if _, err := io.Copy(io.Discard, gate); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for os.Getppid() == job.Parent {
		if time.Now().After(deadline) {
			return errors.New("старый клиент не завершился; файлы не изменены")
		}
		time.Sleep(100 * time.Millisecond)
	}
	result := UpdateResult{BackupPath: filepath.Join(stage, "previous")}
	if err := updates.InstallPrepared(stage, targetDir, runtime.GOARCH); err != nil {
		result.Message = "Обновление не установлено: " + err.Error()
		_ = saveUpdateResult(result)
		return restartFallback(job.Fallback, result.Message)
	}
	// Refresh icons and the menu launcher from the NEW client's embedded assets.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	output, installErr := exec.CommandContext(ctx, target, "--install-desktop").CombinedOutput()
	cancel()
	if installErr == nil {
		result.Message = "Обновление установлено. Предыдущие бинарники сохранены для отката. Системный VPN-helper при необходимости обновите отдельно."
		if err := saveUpdateResult(result); err != nil {
			installErr = err
		}
	}
	if installErr == nil {
		installErr = restartUpdated(target, stage)
	}
	if installErr == nil {
		return nil
	}
	rollbackErr := updates.RestorePrevious(stage, targetDir)
	result.Message = fmt.Sprintf("Новая версия не запустилась; выполнен откат: %v %s", installErr, output)
	if rollbackErr != nil {
		result.Message += "; ошибка отката: " + rollbackErr.Error()
	}
	_ = saveUpdateResult(result)
	if rollbackErr != nil {
		return errors.New(result.Message)
	}
	// Restore launcher/icon metadata too, without granting any privileges.
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	_ = exec.CommandContext(ctx, target, "--install-desktop").Run()
	cancel()
	return restartFallback(job.Fallback, result.Message)
}
