//go:build darwin

package notify

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework UserNotifications
#include <stdlib.h>
void freeturn_notify(const char *title, const char *body, unsigned long long callback);
*/
import "C"
import (
	"errors"
	"github.com/samosvalishe/free-turn-proxy/desktop/locale"
	"net/url"
	"strings"
	"sync"
	"time"
	"unsafe"
)

const throttleWindow = 5 * time.Second

var notificationMu sync.Mutex
var lastCaptchaURL string
var lastCaptchaTime time.Time
var nextID uint64
var callbacks = map[uint64]func(){}

func ShowCaptchaNotification(link string, onOpen func()) error {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(link, "\r\n\x00") {
		return errors.New("invalid captcha URL")
	}
	notificationMu.Lock()
	if lastCaptchaURL == link && time.Since(lastCaptchaTime) < throttleWindow {
		notificationMu.Unlock()
		return nil
	}
	lastCaptchaURL = link
	lastCaptchaTime = time.Now()
	notificationMu.Unlock()
	return show(locale.Text("FreeTurn: требуется решение капчи VK", "FreeTurn: VK captcha verification required"), locale.Text("Нажмите уведомление, чтобы открыть проверку в браузере.", "Click this notification to open verification in your browser."), onOpen)
}
func ShowNotification(title, message, link string) error { return show(title, message, nil) }
func show(title, body string, callback func()) error {
	notificationMu.Lock()
	nextID++
	id := nextID
	if callback != nil {
		callbacks[id] = callback
	}
	notificationMu.Unlock()
	// Discard stale callbacks even if the user never interacts with a banner.
	time.AfterFunc(10*time.Minute, func() { notificationMu.Lock(); delete(callbacks, id); notificationMu.Unlock() })
	a, b := C.CString(title), C.CString(body)
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(b))
	C.freeturn_notify(a, b, C.ulonglong(id))
	return nil
}

//export freeturn_notification_action
func freeturn_notification_action(id C.ulonglong) {
	notificationMu.Lock()
	cb := callbacks[uint64(id)]
	delete(callbacks, uint64(id))
	notificationMu.Unlock()
	if cb != nil {
		go cb()
	}
}
