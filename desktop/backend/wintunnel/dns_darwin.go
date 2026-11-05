//go:build darwin

package wintunnel

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework SystemConfiguration
#include <stdlib.h>
#include <stdint.h>
uintptr_t freeturn_dns_create(const char *name, const char *servers);
void freeturn_dns_release(uintptr_t store);
*/
import "C"
import (
	"errors"
	"strings"
	"unsafe"
)

func installSessionDNS(name string, servers []string) (uintptr, error) {
	n, s := C.CString(name), C.CString(strings.Join(servers, ","))
	defer C.free(unsafe.Pointer(n))
	defer C.free(unsafe.Pointer(s))
	store := uintptr(C.freeturn_dns_create(n, s))
	if store == 0 {
		return 0, errors.New("cannot configure macOS VPN DNS")
	}
	return store, nil
}
func removeSessionDNS(store uintptr) { C.freeturn_dns_release(C.uintptr_t(store)) }
