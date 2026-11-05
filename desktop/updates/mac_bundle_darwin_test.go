//go:build darwin

package updates

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func macTemp(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func macFixture(t *testing.T, root, version string) string {
	t.Helper()
	bundle := filepath.Join(root, "FreeTurn.app")
	for _, dir := range []string{"Contents/MacOS", "Contents/Resources"} {
		if err := os.MkdirAll(filepath.Join(bundle, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{macMain, macHelper} {
		if err = os.WriteFile(filepath.Join(bundle, name), binary, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	info := fmt.Sprintf(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>org.freeturn.desktop</string><key>CFBundleExecutable</key><string>freeturn-desktop</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleShortVersionString</key><string>%s</string><key>CFBundleVersion</key><string>%s</string></dict></plist>`, strings.TrimPrefix(version, "v"), strings.TrimPrefix(version, "v"))
	if err = os.WriteFile(filepath.Join(bundle, "Contents/Info.plist"), []byte(info), 0o644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(bundle, "Contents/Resources/release.txt"), []byte(version), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--force", "--sign", "-", "--timestamp=none", "--identifier", "org.freeturn.helper." + strings.TrimPrefix(version, "v"), filepath.Join(bundle, macHelper)}, {"--force", "--sign", "-", "--timestamp=none", bundle}} {
		if out, err := exec.Command("/usr/bin/codesign", args...).CombinedOutput(); err != nil {
			t.Fatalf("codesign: %s %v", out, err)
		}
	}
	return bundle
}
func macZip(t *testing.T, bundle string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	err := filepath.WalkDir(bundle, func(filename string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(bundle, filename)
		header.Name = "FreeTurn.app/" + filepath.ToSlash(relative)
		header.Method = zip.Deflate
		out, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		input, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer input.Close()
		_, err = io.Copy(out, input)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
func preparedMacFixture(t *testing.T) (string, string) {
	t.Helper()
	parent := macTemp(t)
	target := macFixture(t, parent, "v0.1.0")
	source := macFixture(t, macTemp(t), "v0.2.0")
	stage, err := os.MkdirTemp(parent, macStagePrefix)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(source, filepath.Join(stage, "new.app")); err != nil {
		t.Fatal(err)
	}
	if err = writeMacPrepared(context.Background(), stage, target, "v0.2.0", runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
	return stage, target
}
func TestMacWholeBundleInstallAndRollback(t *testing.T) {
	stage, target := preparedMacFixture(t)
	helperBefore, err := os.ReadFile(filepath.Join(target, macHelper))
	if err != nil {
		t.Fatal(err)
	}
	if err = InstallMacPrepared(stage, target, runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
	helperAfter, err := os.ReadFile(filepath.Join(target, macHelper))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(helperBefore, helperAfter) {
		t.Fatal("VPN helper was not replaced with the app")
	}
	if value, _ := os.ReadFile(filepath.Join(target, "Contents/Resources/release.txt")); string(value) != "v0.2.0" {
		t.Fatal("new bundle was not installed")
	}
	if err = RestoreMacPrevious(stage, target, runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
	if value, _ := os.ReadFile(filepath.Join(target, "Contents/Resources/release.txt")); string(value) != "v0.1.0" {
		t.Fatal("old bundle was not restored")
	}
}
func TestMacReplacementFailureRestoresOldApp(t *testing.T) {
	stage, target := preparedMacFixture(t)
	err := installMacPrepared(stage, target, runtime.GOARCH, func(a, b string) error {
		return fmt.Errorf("injected backup rename failure")
	})
	if err == nil {
		t.Fatal("replacement failure ignored")
	}
	if data, _ := os.ReadFile(filepath.Join(target, "Contents/Resources/release.txt")); string(data) != "v0.1.0" {
		t.Fatal("old app lost after rename failure")
	}
}
func TestMacStagedTamperingAndWrongArchitectureAreRejected(t *testing.T) {
	stage, target := preparedMacFixture(t)
	wrong := "amd64"
	if runtime.GOARCH == wrong {
		wrong = "arm64"
	}
	if err := validateMacBinary(filepath.Join(stage, "new.app", macMain), wrong); err == nil {
		t.Fatal("foreign architecture accepted")
	}
	if err := os.WriteFile(filepath.Join(stage, "new.app/Contents/Resources/release.txt"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMacPrepared(stage, target, runtime.GOARCH); err == nil {
		t.Fatal("staged tampering accepted")
	}
	if data, _ := os.ReadFile(filepath.Join(target, "Contents/Resources/release.txt")); string(data) != "v0.1.0" {
		t.Fatal("installed app changed during validation")
	}
}
func TestMacZIPRejectsUnsafeEntries(t *testing.T) {
	cases := []struct {
		name      string
		mode      os.FileMode
		duplicate bool
	}{
		{"FreeTurn.app/../escape", 0o644, false}, {"/FreeTurn.app/escape", 0o644, false}, {"Other.app/Contents/file", 0o644, false},
		{"FreeTurn.app/Contents/link", os.ModeSymlink | 0o777, false}, {"FreeTurn.app/Contents/file", 0o644, true}, {"__MACOSX/../../escape", 0o644, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent := macTemp(t)
			var data bytes.Buffer
			writer := zip.NewWriter(&data)
			count := 1
			if tc.duplicate {
				count = 2
			}
			for i := 0; i < count; i++ {
				header := &zip.FileHeader{Name: tc.name}
				header.SetMode(tc.mode)
				out, err := writer.CreateHeader(header)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = out.Write([]byte("test"))
			}
			_ = writer.Close()
			archive := filepath.Join(parent, "archive.zip")
			_ = os.WriteFile(archive, data.Bytes(), 0o600)
			if err := extractMacArchive(context.Background(), archive, filepath.Join(parent, "new.app")); err == nil {
				t.Fatalf("unsafe ZIP accepted: %s", tc.name)
			}
		})
	}
}
func TestMacVerifiedDownloadStagesWithoutChangingInstalledApp(t *testing.T) {
	source := macFixture(t, macTemp(t), "v0.2.0")
	archive := macZip(t, source)
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			target := macFixture(t, macTemp(t), "v0.1.0")
			m := NewForPlatform("v0.1.0", runtime.GOARCH, "example/mac", "darwin")
			name := m.archiveName("v0.2.0")
			m.state.Available = true
			m.state.LatestVersion = "v0.2.0"
			m.archive = asset{Name: name, URL: "https://github.com/example/mac/releases/download/v0.2.0/" + name, Size: int64(len(archive))}
			m.checksum = asset{Name: "checksums.txt", URL: "https://github.com/example/mac/releases/download/v0.2.0/checksums.txt"}
			digest := fmt.Sprintf("%x", sha256.Sum256(archive))
			if !valid {
				digest = strings.Repeat("0", 64)
			}
			m.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := archive
				if strings.HasSuffix(r.URL.Path, "checksums.txt") {
					body = []byte(digest + "  " + name + "\n")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
			})
			stage, err := m.PrepareMacInstall(context.Background(), target)
			if (err == nil) != valid {
				t.Fatalf("verified=%v: %v", valid, err)
			}
			if valid {
				if err = ValidateMacPrepared(stage, target, runtime.GOARCH); err != nil {
					t.Fatal(err)
				}
			} else {
				entries, _ := filepath.Glob(filepath.Join(filepath.Dir(target), macStagePrefix+"*"))
				if len(entries) != 0 {
					t.Fatal("failed staging was not cleaned")
				}
			}
			if value, _ := os.ReadFile(filepath.Join(target, "Contents/Resources/release.txt")); string(value) != "v0.1.0" {
				t.Fatal("installed bundle changed before shutdown")
			}
		})
	}
}
func TestMacPackagedArchive(t *testing.T) {
	archive := os.Getenv("FREETURN_TEST_MACOS_ARCHIVE")
	if archive == "" {
		t.Skip("set to the packaged release ZIP in CI")
	}
	version := os.Getenv("FREETURN_TEST_MACOS_VERSION")
	if version == "" {
		t.Fatal("missing packaged version")
	}
	parent := macTemp(t)
	target := macFixture(t, parent, "v0.1.0")
	stage, err := os.MkdirTemp(parent, macStagePrefix)
	if err != nil {
		t.Fatal(err)
	}
	if err = extractMacArchive(context.Background(), archive, filepath.Join(stage, "new.app")); err != nil {
		t.Fatal(err)
	}
	if err = writeMacPrepared(context.Background(), stage, target, version, runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
	if err = ValidateMacPrepared(stage, target, runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(filepath.Join(stage, "new.app", macMain), "--update-protocol").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != MacUpdateProtocolMarker {
		t.Fatalf("packaged updater protocol: %q: %v", output, err)
	}
	t.Log("packaged ZIP extracts to a complete valid signed app and passes updater validation")
}
