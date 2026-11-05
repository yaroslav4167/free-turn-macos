//go:build darwin

package main

import (
	"fmt"
	"github.com/samosvalishe/free-turn-proxy/desktop/backend/vpnservice"
	"github.com/samosvalishe/free-turn-proxy/desktop/backend/wintunnel"
	"os"
)

func run() error {
	_ = os.Unsetenv("WG_TUN_NAME_FILE")
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: freeturn-vpn-helper --service|--install-service|--remove-service UID")
	}
	// Keep the old private-session entry point for compatibility only.
	if os.Args[1] == "--vpn-helper" {
		return wintunnel.RunHelper(os.Args[2])
	}
	uid, err := wintunnel.ParseServiceUID(os.Args[2])
	if err != nil {
		return err
	}
	switch os.Args[1] {
	case "--service":
		return wintunnel.RunService(uid)
	case "--install-service":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		return vpnservice.Install(uid, exe)
	case "--remove-service":
		return vpnservice.Remove(uid)
	default:
		return fmt.Errorf("unknown VPN helper command")
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
