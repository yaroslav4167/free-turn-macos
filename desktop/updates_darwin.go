//go:build darwin

package main

import (
	"errors"
	"os"

	"github.com/samosvalishe/free-turn-proxy/desktop/integration"
	"github.com/samosvalishe/free-turn-proxy/desktop/locale"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) installPlatformUpdate() (bool, error) { return true, a.installMacUpdate() }
func (a *App) installMacUpdate() error {
	if !a.updateInstallMu.TryLock() {
		return errors.New("установка уже выполняется")
	}
	defer a.updateInstallMu.Unlock()
	if os.Geteuid() == 0 {
		return errors.New("обновление клиента нельзя запускать от root")
	}
	state := a.updater.State()
	if !state.Available || state.Busy != "" {
		return errors.New("сначала проверьте доступное обновление")
	}
	target, err := integration.MacUpdateTarget()
	if err != nil {
		return err
	}
	install := locale.Text("Установить и перезапустить", "Install and restart")
	cancel := locale.Text("Отмена", "Cancel")
	answer, err := wailsruntime.MessageDialog(a.ctx, wailsruntime.MessageDialogOptions{
		Type: wailsruntime.QuestionDialog, Title: locale.Text("Установить обновление FreeTurn?", "Install FreeTurn update?"),
		Message: locale.Text("Скачать и проверить "+state.LatestVersion+", отключить VPN и перезапустить FreeTurn? Приложение будет заменено целиком, вместе с VPN-helper. Предыдущая версия сохранится для отката. Профили и настройки сохранятся.", "Download and verify "+state.LatestVersion+", disconnect the VPN and restart FreeTurn? The entire app, including its VPN helper, will be replaced. The previous version will be retained for rollback. Profiles and settings are preserved."),
		Buttons: []string{install, cancel}, DefaultButton: cancel, CancelButton: cancel,
	})
	if err != nil {
		return err
	}
	if answer != install {
		return nil
	}
	stage, err := a.updater.PrepareMacInstall(a.updateContext, target)
	if err != nil {
		return err
	}
	var gate *os.File
	err = a.updater.Restart(func() error {
		var startErr error
		gate, startErr = integration.StartUpdateWorker(stage)
		return startErr
	})
	if err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	a.desktopMu.Lock()
	a.updateInstallGate = gate
	a.desktopMu.Unlock()
	a.Quit()
	return nil
}
