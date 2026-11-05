//go:build !windows && !linux && !darwin

package notify

func showCustomCaptchaPopup(captchaURL string) error {
	return errUnsupported
}
