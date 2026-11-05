//go:build darwin

package main

import (
	"fmt"
	"github.com/samosvalishe/free-turn-proxy/desktop/backend/wintunnel"
	"os"
)

func main() {
	_ = os.Unsetenv("WG_TUN_NAME_FILE")
	if len(os.Args) != 3 || os.Args[1] != "--vpn-helper" {
		fmt.Fprintln(os.Stderr, "usage: freeturn-vpn-helper --vpn-helper SOCKET")
		os.Exit(2)
	}
	if err := wintunnel.RunHelper(os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
