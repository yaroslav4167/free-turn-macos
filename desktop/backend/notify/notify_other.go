//go:build !windows && !linux && !darwin

package notify

import (
	"errors"
	"time"
)

var (
	errUnsupported = errors.New("notify: desktop notifications are only implemented on Windows")

	throttleWindow = 5 * time.Second
)

// ShowCaptchaNotification is a no-op / unsupported stub on non-Windows platforms.
func ShowCaptchaNotification(captchaURL string, onOpen func()) error {
	return errUnsupported
}

// ShowNotification is a no-op / unsupported stub on non-Windows platforms.
func ShowNotification(title, message, url string) error {
	return errUnsupported
}
