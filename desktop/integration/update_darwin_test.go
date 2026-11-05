//go:build darwin

package integration

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/samosvalishe/free-turn-proxy/desktop/updates"
)

// The signed fixture is this test binary. Its child mode acknowledges startup
// through the same inherited pipe as the real GUI, without opening a window.
func TestMain(m *testing.M) {
	if os.Getenv("FREETURN_TEST_UPDATE_CHILD") == "1" {
		switch {
		case len(os.Args) == 2 && os.Args[1] == "--updated":
			if os.Getenv("FREETURN_TEST_UPDATE_FAIL") == "1" {
				os.Exit(1)
			}
			HandleUpdateProcess()
			AcknowledgeUpdate()
			os.Exit(0)
		case len(os.Args) == 3 && os.Args[1] == "--test-launch-update":
			target, err := bundlePath()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			executable, _ := os.Executable()
			gate, err := startMacUpdateWorker(os.Args[2], target, executable)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			_ = gate.Close()
			os.Exit(0)
		case len(os.Args) == 3 && os.Args[1] == "--apply-update":
			err := runMacUpdateWorker(os.Args[2])
			message := "OK"
			if err != nil {
				message = err.Error()
			}
			_ = os.WriteFile(filepath.Join(os.Args[2], "test-worker-finished"), []byte(message), 0o600)
			if err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		case len(os.Args) == 1:
			os.Exit(0) // The restored old GUI in this isolated test needs no ACK.
		default:
			os.Exit(2)
		}
	}
	os.Exit(m.Run())
}
func updateFixture(t *testing.T, parent, version string) string {
	t.Helper()
	bundle := filepath.Join(parent, "FreeTurn.app")
	for _, dir := range []string{"Contents/MacOS", "Contents/Resources"} {
		if err := os.MkdirAll(filepath.Join(bundle, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"freeturn-desktop", "freeturn-vpn-helper"} {
		if err = os.WriteFile(filepath.Join(bundle, "Contents/MacOS", name), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	info := fmt.Sprintf(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>org.freeturn.desktop</string><key>CFBundleExecutable</key><string>freeturn-desktop</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleShortVersionString</key><string>%s</string><key>CFBundleVersion</key><string>%s</string></dict></plist>`, strings.TrimPrefix(version, "v"), strings.TrimPrefix(version, "v"))
	_ = os.WriteFile(filepath.Join(bundle, "Contents/Info.plist"), []byte(info), 0o644)
	_ = os.WriteFile(filepath.Join(bundle, "Contents/Resources/version"), []byte(version), 0o644)
	for _, target := range []string{filepath.Join(bundle, "Contents/MacOS/freeturn-vpn-helper"), bundle} {
		if out, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", "--timestamp=none", target).CombinedOutput(); err != nil {
			t.Fatalf("codesign: %s %v", out, err)
		}
	}
	return bundle
}
func updateStage(t *testing.T) (string, string) {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := updateFixture(t, parent, "v0.1.0")
	stage, err := os.MkdirTemp(parent, ".freeturn-mac-update-")
	if err != nil {
		t.Fatal(err)
	}
	source := updateFixture(t, t.TempDir(), "v0.2.0")
	if err = os.Rename(source, filepath.Join(stage, "new.app")); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	err = filepath.WalkDir(filepath.Join(stage, "new.app"), func(filename string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer file.Close()
		hash := sha256.New()
		if _, err = io.Copy(hash, file); err != nil {
			return err
		}
		relative, _ := filepath.Rel(filepath.Join(stage, "new.app"), filename)
		files[filepath.ToSlash(relative)] = fmt.Sprintf("%x", hash.Sum(nil))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"version": "v0.2.0", "arch": runtime.GOARCH, "target": target, "files": files})
	if err = os.WriteFile(filepath.Join(stage, "mac-plan.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = updates.ValidateMacPrepared(stage, target, runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
	return stage, target
}
func TestMacUpdateLaunchAndAutomaticRollback(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(fmt.Sprint(healthy), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("FREETURN_TEST_UPDATE_CHILD", "1")
			fail := "0"
			if !healthy {
				fail = "1"
			}
			t.Setenv("FREETURN_TEST_UPDATE_FAIL", fail)
			stage, target := updateStage(t)
			profile := filepath.Join(os.Getenv("HOME"), "profile.json")
			_ = os.WriteFile(profile, []byte("preserve my profiles"), 0o600)
			launcher := exec.Command(filepath.Join(target, "Contents/MacOS/freeturn-desktop"), "--test-launch-update", stage)
			if out, err := launcher.CombinedOutput(); err != nil {
				t.Fatalf("launcher: %s %v", out, err)
			}
			deadline := time.Now().Add(20 * time.Second)
			for {
				finished, err := os.ReadFile(filepath.Join(stage, "test-worker-finished"))
				if err == nil {
					if string(finished) != "OK" {
						log, _ := os.ReadFile(filepath.Join(stage, "worker.log"))
						t.Fatalf("worker failed: %s\n%s", finished, log)
					}
					break
				}
				if time.Now().After(deadline) {
					log, _ := os.ReadFile(filepath.Join(stage, "worker.log"))
					t.Fatalf("worker did not finish: %s", log)
				}
				time.Sleep(20 * time.Millisecond)
			}
			want := "v0.2.0"
			if !healthy {
				want = "v0.1.0"
			}
			actual, _ := os.ReadFile(filepath.Join(target, "Contents/Resources/version"))
			if string(actual) != want {
				t.Fatalf("installed %s, expected %s", actual, want)
			}
			if healthy {
				if _, err := os.Stat(updates.MacPreviousPath(stage)); err != nil {
					t.Fatal("backup not retained")
				}
			}
			result := LoadUpdateResult()
			if healthy && !strings.HasPrefix(result.Message, "Обновление установлено.") {
				t.Fatalf("missing success result: %+v", result)
			}
			if !healthy && !strings.Contains(result.Message, "откат") {
				t.Fatalf("missing rollback result: %+v", result)
			}
			if data, _ := os.ReadFile(profile); string(data) != "preserve my profiles" {
				t.Fatal("profile data changed")
			}
		})
	}
}
func TestMacGUIAcknowledgementRequiresExplicitDOMReady(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	updateAck = write
	t.Cleanup(func() { _ = write.Close(); updateAck = nil })
	AcknowledgeUpdate()
	marker := []byte{0}
	if _, err = io.ReadFull(read, marker); err != nil || marker[0] != 1 {
		t.Fatalf("missing readiness marker: %v", err)
	}
	if updateAck != nil {
		t.Fatal("acknowledgement pipe retained")
	}
	AcknowledgeUpdate()
}
