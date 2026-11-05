//go:build darwin

package integration

import (
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

type macUpdateJob struct {
	Parent   int    `json:"parent"`
	Target   string `json:"target"`
	Fallback string `json:"fallback"`
}

func MacUpdateTarget() (string, error) {
	bundle, err := bundlePath()
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if filepath.Dir(bundle) != "/Applications" && filepath.Dir(bundle) != filepath.Join(home, "Applications") {
		return "", errors.New("для обновлений переместите FreeTurn.app в /Applications или ~/Applications")
	}
	return bundle, updates.ValidateMacTarget(bundle)
}
func StartUpdateWorker(stage string) (*os.File, error) {
	target, err := MacUpdateTarget()
	if err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return startMacUpdateWorker(stage, target, executable)
}
func startMacUpdateWorker(stage, target, executable string) (*os.File, error) {
	if os.Geteuid() == 0 {
		return nil, errors.New("updater cannot run as root")
	}
	if err := updates.ValidateMacPrepared(stage, target, runtime.GOARCH); err != nil {
		return nil, err
	}
	if executable != filepath.Join(target, "Contents", "MacOS", "freeturn-desktop") {
		return nil, errors.New("неверный исполняемый файл установленного приложения")
	}
	data, _ := json.Marshal(macUpdateJob{Parent: os.Getpid(), Target: target, Fallback: executable})
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
		_ = gateWrite.Close()
		return nil, err
	}
	defer readyRead.Close()
	defer readyWrite.Close()
	log, err := os.OpenFile(filepath.Join(stage, "worker.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		_ = gateWrite.Close()
		return nil, err
	}
	defer log.Close()
	cmd := exec.Command(executable, "--apply-update", stage)
	cmd.ExtraFiles = []*os.File{gateRead, readyWrite}
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		_ = gateWrite.Close()
		return nil, err
	}
	_ = readyWrite.Close()
	_ = readyRead.SetReadDeadline(time.Now().Add(15 * time.Second))
	marker := []byte{0}
	if _, err = io.ReadFull(readyRead, marker); err != nil || marker[0] != 1 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = gateWrite.Close()
		return nil, errors.New("процесс обновления не подтвердил готовность; приложение не закрыто")
	}
	_ = cmd.Process.Release()
	return gateWrite, nil
}
func HandleUpdateProcess() bool {
	if len(os.Args) == 2 && os.Args[1] == "--update-protocol" {
		fmt.Println(updates.MacUpdateProtocolMarker)
		return true
	}
	if len(os.Args) == 3 && os.Args[1] == "--apply-update" {
		if err := runMacUpdateWorker(os.Args[2]); err != nil {
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
func runMacUpdateWorker(stage string) error {
	if os.Geteuid() == 0 {
		return errors.New("updater cannot run as root")
	}
	target, err := bundlePath()
	if err != nil {
		return err
	}
	if err = updates.ValidateMacPrepared(stage, target, runtime.GOARCH); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(stage, "worker.json"))
	if err != nil {
		return err
	}
	var job macUpdateJob
	if len(data) > 16384 {
		return errors.New("invalid update job")
	}
	if err = json.Unmarshal(data, &job); err != nil {
		return err
	}
	if job.Parent != os.Getppid() || job.Target != target || job.Fallback != filepath.Join(target, "Contents", "MacOS", "freeturn-desktop") {
		return errors.New("invalid update parent or target")
	}
	gate, ready := os.NewFile(3, "update-gate"), os.NewFile(4, "update-ready")
	if gate == nil || ready == nil {
		return errors.New("missing update pipes")
	}
	defer gate.Close()
	if _, err = ready.Write([]byte{1}); err != nil {
		_ = ready.Close()
		return err
	}
	_ = ready.Close()
	_ = gate.SetReadDeadline(time.Now().Add(2 * time.Minute))
	if _, err = io.Copy(io.Discard, gate); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for os.Getppid() == job.Parent {
		if time.Now().After(deadline) {
			return errors.New("старый клиент не завершился; приложение не изменено")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return applyMacUpdate(stage, target, runtime.GOARCH, restartUpdated, restartFallback, saveUpdateResult)
}
func applyMacUpdate(stage, target, arch string, restart func(string, string) error, fallback func(string, string) error, save func(UpdateResult) error) error {
	result := UpdateResult{BackupPath: updates.MacPreviousPath(stage)}
	old, err := os.Lstat(target)
	if err != nil {
		return err
	}
	executable := filepath.Join(target, "Contents", "MacOS", "freeturn-desktop")
	if err = updates.InstallMacPrepared(stage, target, arch); err != nil {
		result.Message = "Обновление не установлено: " + err.Error()
		_ = save(result)
		current, statErr := os.Lstat(target)
		if statErr != nil || !os.SameFile(old, current) {
			return errors.New(result.Message)
		}
		return fallback(executable, result.Message)
	}
	result.Message = "Обновление установлено. Предыдущая версия сохранена для отката."
	if err = save(result); err == nil {
		err = restart(executable, stage)
	}
	if err == nil {
		return nil
	}
	rollbackErr := updates.RestoreMacPrevious(stage, target, arch)
	result.Message = fmt.Sprintf("Новая версия не запустилась; выполнен откат: %v", err)
	if rollbackErr != nil {
		result.Message += "; ошибка отката: " + rollbackErr.Error()
	} else {
		result.BackupPath = target
	}
	_ = save(result)
	if rollbackErr != nil {
		return errors.New(result.Message)
	}
	return fallback(executable, result.Message)
}
