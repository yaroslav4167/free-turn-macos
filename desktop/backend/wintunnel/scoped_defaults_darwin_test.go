//go:build darwin

package wintunnel

import (
	"errors"
	"github.com/samosvalishe/free-turn-proxy/mobile"
	"reflect"
	"strings"
	"testing"
)

func TestPhysicalRouteGatewayParsing(t *testing.T) {
	cases := []struct {
		out  string
		ipv6 bool
		want physicalNetworkRoute
	}{
		{"gateway: 192.0.2.1\ninterface: en0\nflags: <UP,GATEWAY,DONE,STATIC,GLOBAL>", false, physicalNetworkRoute{device: "en0", gateway: "192.0.2.1"}},
		{"gateway: fe80::1%en1\ninterface: en1\nflags: <UP,GATEWAY,DONE>", true, physicalNetworkRoute{device: "en1", gateway: "fe80::1%en1", ipv6: true}},
		{"gateway: link#8\ninterface: ppp0\nflags: <UP,DONE,STATIC>", false, physicalNetworkRoute{device: "ppp0", direct: true}},
	}
	for _, tc := range cases {
		got, err := parsePhysicalNetworkRoute(tc.out, tc.ipv6)
		if err != nil || got != tc.want {
			t.Fatalf("%q: %+v %v", tc.out, got, err)
		}
	}
	for _, bad := range []string{"gateway: broken\ninterface: en0\nflags: <GATEWAY>", "gateway: 192.0.2.1\ninterface: utun3", "gateway: 192.0.2.1", "gateway: fe80::1%en1\ninterface: en0"} {
		if _, err := parsePhysicalNetworkRoute(bad, false); err == nil {
			t.Fatalf("invalid gateway accepted: %q", bad)
		}
	}
}
func TestScopedDefaultRequiresExplicitScope(t *testing.T) {
	for _, flags := range []string{"<UP,GATEWAY,GLOBAL>", "<UP,GATEWAY,IFSCOPE>"} {
		out := "destination: default\ninterface: en0\nflags: " + flags
		if got := scopedDefaultExists(out, "en0"); got != strings.Contains(flags, "IFSCOPE") {
			t.Fatalf("scope detection: %q", out)
		}
		if scopedDefaultExists(out, "en1") {
			t.Fatal("wrong physical interface accepted")
		}
	}
	if scopedDefaultExists("destination: 0.0.0.0\nmask: 128.0.0.0\ninterface: en0\nflags: <UP,IFSCOPE>", "en0") {
		t.Fatal("VPN half treated as physical default")
	}
}
func TestScopedPhysicalRoutePrecedesVPNAndOnlyOwnedRoutesAreRemoved(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "preserve"}[exists], func(t *testing.T) {
			physical := physicalNetworkRoute{device: "en0", gateway: "192.0.2.1"}
			var calls [][]string
			state := &helperState{name: "utun9", physical4: physical}
			state.run = func(_ string, args ...string) (string, error) {
				calls = append(calls, append([]string{}, args...))
				if args[1] == "get" {
					flags := "<UP,GATEWAY,GLOBAL>"
					if exists {
						flags = "<UP,GATEWAY,IFSCOPE>"
					}
					return "destination: default\ninterface: en0\nflags: " + flags, nil
				}
				return "", nil
			}
			if err := state.installRoutes(&mobile.TunnelParams{AllowedIPs: "0.0.0.0/0"}); err != nil {
				t.Fatal(err)
			}
			offset := 1
			if !exists {
				if !reflect.DeepEqual(calls[1], physical.defaultArgs("add")) {
					t.Fatalf("physical route was not installed first: %v", calls)
				}
				offset++
			}
			if !reflect.DeepEqual(calls[offset], []string{"-n", "add", "-inet", "0.0.0.0/1", "-interface", "utun9"}) {
				t.Fatalf("VPN route order: %v", calls)
			}
			count := len(calls)
			state.cleanup()
			removed := calls[count:]
			want := 2
			if !exists {
				want++
			}
			if len(removed) != want {
				t.Fatalf("removed another owner's routes: %v", removed)
			}
			if !exists && !reflect.DeepEqual(removed[len(removed)-1], physical.defaultArgs("delete")) {
				t.Fatalf("physical route removed before VPN routes: %v", removed)
			}
			count = len(calls)
			state.cleanup()
			if len(calls) != count {
				t.Fatal("cleanup is not idempotent")
			}
		})
	}
}
func TestScopedPhysicalRouteFailureDoesNotInstallVPNRoutes(t *testing.T) {
	state := &helperState{physical4: physicalNetworkRoute{device: "en0", gateway: "192.0.2.1"}}
	state.run = func(_ string, args ...string) (string, error) {
		if args[1] == "add" && args[4] != "en0" {
			t.Fatalf("VPN routes installed after failed physical route: %v", args)
		}
		return "", errors.New("route unavailable")
	}
	if err := state.installRoutes(&mobile.TunnelParams{AllowedIPs: "0.0.0.0/0"}); err == nil {
		t.Fatal("missing physical route ignored")
	}
	if len(state.routes) != 0 {
		t.Fatal("failed route claimed as owned")
	}
}
