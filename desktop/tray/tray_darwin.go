//go:build darwin

package tray

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void freeturn_tray_start(const char *title, const char *open, const char *quit);
void freeturn_tray_close(void);
void freeturn_tray_language(const char *open, const char *quit);
*/
import "C"
import (
	"errors"
	"github.com/samosvalishe/free-turn-proxy/desktop/locale"
	"sync"
	"sync/atomic"
	"unsafe"
)

type Tray struct {
	onShow, onQuit func()
	available      atomic.Bool
}

var trayMu sync.Mutex
var currentTray *Tray

func Start(title string, onShow, onQuit func()) (*Tray, error) {
	trayMu.Lock()
	defer trayMu.Unlock()
	if currentTray != nil {
		return nil, errors.New("menu bar item already exists")
	}
	t := &Tray{onShow: onShow, onQuit: onQuit}
	t.available.Store(true)
	currentTray = t
	a, b, c := C.CString(title), C.CString(locale.Text("Открыть FreeTurn", "Open FreeTurn")), C.CString(locale.Text("Выйти", "Quit"))
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(b))
	defer C.free(unsafe.Pointer(c))
	C.freeturn_tray_start(a, b, c)
	return t, nil
}
func (t *Tray) Available() bool { return t != nil && t.available.Load() }
func (t *Tray) Close() {
	trayMu.Lock()
	defer trayMu.Unlock()
	if currentTray == t {
		t.available.Store(false)
		C.freeturn_tray_close()
		currentTray = nil
	}
}
func (t *Tray) RefreshLanguage() {
	if !t.Available() {
		return
	}
	a, b := C.CString(locale.Text("Открыть FreeTurn", "Open FreeTurn")), C.CString(locale.Text("Выйти", "Quit"))
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(b))
	C.freeturn_tray_language(a, b)
}

//export freeturn_tray_action
func freeturn_tray_action(action C.int) {
	trayMu.Lock()
	t := currentTray
	var callback func()
	if t != nil {
		if action == 1 {
			callback = t.onShow
		} else if action == 2 {
			callback = t.onQuit
		}
	}
	trayMu.Unlock()
	if callback != nil {
		go callback()
	}
}
