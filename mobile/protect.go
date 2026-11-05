package mobile

import (
	"errors"
	"sync/atomic"
	"syscall"

	"github.com/samosvalishe/free-turn-proxy/internal/netctl"
)

// Protector реализуется хостом для исключения сокетов клиента из VPN-туннеля (VpnService.protect).
// Без этого TURN / VK API / DNS трафик заворачивается обратно в туннель.
type Protector interface {
	Protect(fd int) bool
}

var protector atomic.Pointer[Protector]

// SetProtect устанавливает обработчик защиты сокетов хоста (nil - no-op).
func SetProtect(p Protector) {
	if p == nil {
		protector.Store(nil)
		netctl.SetControl(nil)
		return
	}
	protector.Store(&p)
	netctl.SetControl(func(_, _ string, c syscall.RawConn) error {
		protected := false
		if err := c.Control(func(fd uintptr) { protected = p.Protect(int(fd)) }); err != nil {
			return err
		}
		if !protected {
			return errors.New("cannot protect socket from VPN routing")
		}
		return nil
	})
}

func protectFD(fd int) bool {
	if p := protector.Load(); p != nil {
		return (*p).Protect(fd)
	}
	return false
}
