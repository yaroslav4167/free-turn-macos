//go:build darwin

package updates

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"debug/macho"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const MacUpdateProtocolMarker = "FREETURN_MAC_UPDATE_PROTOCOL_V1"
const macStagePrefix = ".freeturn-mac-update-"
const macMain = "Contents/MacOS/freeturn-desktop"
const macHelper = "Contents/MacOS/freeturn-vpn-helper"

type macPlan struct {
	Version   string            `json:"version"`
	Arch      string            `json:"arch"`
	Target    string            `json:"target"`
	Files     map[string]string `json:"files"`
	NewDevice uint64            `json:"newDevice"`
	NewInode  uint64            `json:"newInode"`
}

// PrepareMacInstall verifies the download and the complete signed app bundle
// before shutdown. Stage and target share a filesystem for atomic renames.
func (m *Manager) PrepareMacInstall(ctx context.Context, target string) (stage string, err error) {
	if err = m.begin("preparing"); err != nil {
		return "", err
	}
	defer func() { m.finish(err) }()
	state := m.State()
	if m.platform != "darwin" || !state.Available {
		return "", errors.New("сначала проверьте доступное обновление macOS")
	}
	if err = ValidateMacTarget(target); err != nil {
		return "", err
	}
	stage, err = os.MkdirTemp(filepath.Dir(target), macStagePrefix)
	if err != nil {
		return "", fmt.Errorf("нет прав на обновление приложения: %w", err)
	}
	owned := stage
	defer func() {
		if err != nil {
			_ = os.RemoveAll(owned)
		}
		m.mu.Lock()
		if m.state.DownloadedPath == filepath.Join(owned, "archive.zip") {
			m.state.DownloadedPath = ""
		}
		m.mu.Unlock()
	}()
	archive := filepath.Join(stage, "archive.zip")
	if err = m.download(ctx, archive, state.LatestVersion); err != nil {
		return "", err
	}
	m.setPhase("extracting")
	if err = extractMacArchive(ctx, archive, filepath.Join(stage, "new.app")); err != nil {
		return "", err
	}
	if err = writeMacPrepared(ctx, stage, target, state.LatestVersion, m.arch); err != nil {
		return "", err
	}
	if err = os.Remove(archive); err != nil {
		return "", err
	}
	return stage, nil
}

func ValidateMacTarget(target string) error {
	if !filepath.IsAbs(target) || filepath.Clean(target) != target || !strings.HasSuffix(target, ".app") {
		return errors.New("недопустимый путь приложения")
	}
	parent := filepath.Dir(target)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		return errors.New("каталог приложения не должен содержать символические ссылки")
	}
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() {
		return errors.New("установленное приложение отсутствует или является ссылкой")
	}
	return nil
}

func extractMacArchive(ctx context.Context, archive, destination string) error {
	input, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer input.Close()
	if len(input.File) > 8192 {
		return errors.New("слишком много файлов в архиве")
	}
	if err = os.Mkdir(destination, 0o755); err != nil {
		return err
	}
	seen := map[string]bool{}
	var total uint64
	for _, entry := range input.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name
		if strings.ContainsAny(name, "\\\x00") || path.IsAbs(name) {
			return errors.New("небезопасный путь в архиве")
		}
		for _, component := range strings.Split(name, "/") {
			if component == ".." {
				return errors.New("выход за пределы приложения запрещён")
			}
		}
		clean := path.Clean(name)
		if clean == "__MACOSX" || strings.HasPrefix(clean, "__MACOSX/") {
			continue
		} // ditto's AppleDouble metadata, never executable bundle files
		if clean == "FreeTurn.app" {
			if !entry.FileInfo().IsDir() {
				return errors.New("неверный корень приложения")
			}
			continue
		}
		if !strings.HasPrefix(clean, "FreeTurn.app/") {
			return fmt.Errorf("посторонний файл в архиве: %s", clean)
		}
		relative := strings.TrimPrefix(clean, "FreeTurn.app/")
		if seen[relative] {
			return errors.New("повторяющийся путь в архиве")
		}
		seen[relative] = true
		mode := entry.Mode()
		if !mode.IsRegular() && !mode.IsDir() {
			return errors.New("ссылки и специальные файлы в обновлении запрещены")
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		if mode.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		total += entry.UncompressedSize64
		if entry.UncompressedSize64 > maxArchive || total > 1<<30 {
			return errors.New("слишком большой распакованный архив")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		permissions := os.FileMode(0o644)
		if relative == macMain || relative == macHelper {
			permissions = 0o755
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, permissions)
		if err != nil {
			return err
		}
		source, err := entry.Open()
		if err != nil {
			_ = output.Close()
			return err
		}
		count, copyErr := io.Copy(output, contextReader{ctx, io.LimitReader(source, int64(entry.UncompressedSize64)+1)})
		err = errors.Join(copyErr, source.Close(), output.Sync(), output.Close())
		if err != nil {
			return err
		}
		if uint64(count) != entry.UncompressedSize64 {
			return errors.New("размер файла обновления не совпадает")
		}
	}
	return nil
}

func macBundleFiles(bundle string) (map[string]string, error) {
	files := map[string]string{}
	var total int64
	err := filepath.WalkDir(bundle, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("приложение содержит ссылку или специальный файл")
		}
		total += info.Size()
		if info.Size() > maxArchive || total > 1<<30 || len(files) >= 8192 {
			return errors.New("приложение превышает ограничения размера")
		}
		file, err := os.Open(filename)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		err = errors.Join(copyErr, file.Close())
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(bundle, filename)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = hex.EncodeToString(hash.Sum(nil))
		return nil
	})
	return files, err
}
func macCommand(ctx context.Context, executable string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, executable, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", filepath.Base(executable), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
func validateMacBinary(filename, arch string) error {
	expected, ok := map[string]macho.Cpu{"arm64": macho.CpuArm64, "amd64": macho.CpuAmd64}[arch]
	if !ok {
		return errors.New("неподдерживаемая архитектура macOS")
	}
	binary, err := macho.Open(filename)
	if err != nil {
		return errors.New("файл не является Mach-O приложением")
	}
	defer binary.Close()
	if binary.Cpu != expected || binary.Type != macho.TypeExec {
		return errors.New("исполняемый файл предназначен для другой архитектуры")
	}
	info, err := os.Stat(filename)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o111 == 0 {
		return errors.New("файл не имеет разрешения на запуск")
	}
	return nil
}
func validateMacBundle(ctx context.Context, bundle, version, arch string) error {
	data, err := macCommand(ctx, "/usr/bin/plutil", "-convert", "json", "-o", "-", filepath.Join(bundle, "Contents/Info.plist"))
	if err != nil {
		return err
	}
	var info map[string]any
	if err = json.Unmarshal(data, &info); err != nil {
		return err
	}
	if info["CFBundleIdentifier"] != "org.freeturn.desktop" || info["CFBundleExecutable"] != "freeturn-desktop" || info["CFBundleShortVersionString"] != strings.TrimPrefix(version, "v") {
		return errors.New("идентификатор или версия приложения не совпадает с релизом")
	}
	for _, name := range []string{macMain, macHelper} {
		if err := validateMacBinary(filepath.Join(bundle, name), arch); err != nil {
			return err
		}
	}
	if !supportsMarker(filepath.Join(bundle, macMain), MacUpdateProtocolMarker) {
		return errors.New("новая версия не поддерживает безопасное обновление macOS")
	}
	_, err = macCommand(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", bundle)
	return err
}
func writeMacPrepared(ctx context.Context, stage, target, version, arch string) error {
	if _, ok := parseVersion(version); !ok {
		return errors.New("недопустимая версия")
	}
	bundle := filepath.Join(stage, "new.app")
	files, err := macBundleFiles(bundle)
	if err != nil {
		return err
	}
	if err = validateMacBundle(ctx, bundle, version, arch); err != nil {
		return err
	}
	return writeMacPlan(stage, macPlan{Version: version, Arch: arch, Target: target, Files: files})
}
func writeMacPlan(stage string, plan macPlan) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(stage, ".mac-plan-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(stage, "mac-plan.json"))
}
func readMacPlan(stage, target, arch string) (macPlan, error) {
	var plan macPlan
	if !filepath.IsAbs(stage) || filepath.Dir(stage) != filepath.Dir(target) || !strings.HasPrefix(filepath.Base(stage), macStagePrefix) {
		return plan, errors.New("недопустимый каталог обновления macOS")
	}
	info, err := os.Lstat(stage)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return plan, errors.New("небезопасный каталог обновления macOS")
	}
	file, err := os.Open(filepath.Join(stage, "mac-plan.json"))
	if err != nil {
		return plan, err
	}
	defer file.Close()
	if err = json.NewDecoder(io.LimitReader(file, 2<<20)).Decode(&plan); err != nil {
		return plan, err
	}
	if plan.Target != target || plan.Arch != arch || len(plan.Files) == 0 {
		return plan, errors.New("план обновления относится к другому приложению")
	}
	if _, ok := parseVersion(plan.Version); !ok {
		return plan, errors.New("неверная версия обновления")
	}
	return plan, nil
}
func ValidateMacPrepared(stage, target, arch string) error {
	if err := ValidateMacTarget(target); err != nil {
		return err
	}
	plan, err := readMacPlan(stage, target, arch)
	if err != nil {
		return err
	}
	files, err := macBundleFiles(filepath.Join(stage, "new.app"))
	if err != nil {
		return err
	}
	if len(files) != len(plan.Files) {
		return errors.New("состав приложения изменён после проверки")
	}
	for name, digest := range files {
		if plan.Files[name] != digest {
			return errors.New("файл приложения изменён после проверки")
		}
	}
	return validateMacBundle(context.Background(), filepath.Join(stage, "new.app"), plan.Version, arch)
}
func MacPreviousPath(stage string) string { return filepath.Join(stage, "previous.app") }

// InstallMacPrepared replaces the whole bundle, including its VPN helper.
func InstallMacPrepared(stage, target, arch string) error {
	return installMacPrepared(stage, target, arch, os.Rename)
}
func installMacPrepared(stage, target, arch string, rename func(string, string) error) error {
	if err := ValidateMacPrepared(stage, target, arch); err != nil {
		return err
	}
	plan, err := readMacPlan(stage, target, arch)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(MacPreviousPath(stage)); !os.IsNotExist(err) {
		return errors.New("резервная копия уже существует")
	}
	info, err := os.Lstat(filepath.Join(stage, "new.app"))
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("не удалось определить подготовленное приложение")
	}
	plan.NewDevice, plan.NewInode = uint64(stat.Dev), stat.Ino
	if err = writeMacPlan(stage, plan); err != nil {
		return err
	}
	candidate := filepath.Join(stage, "new.app")
	if err = unix.RenamexNp(candidate, target, unix.RENAME_SWAP); err != nil {
		return err
	}
	if err = rename(candidate, MacPreviousPath(stage)); err != nil {
		return errors.Join(err, unix.RenamexNp(candidate, target, unix.RENAME_SWAP))
	}

	return nil
}
func RestoreMacPrevious(stage, target, arch string) error {
	plan, err := readMacPlan(stage, target, arch)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(MacPreviousPath(stage)); err != nil {
		return err
	}
	if info, statErr := os.Lstat(target); statErr == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || !ok || uint64(stat.Dev) != plan.NewDevice || stat.Ino != plan.NewInode {
			return errors.New("приложение изменено другим процессом; резервная копия сохранена")
		}
		if err = unix.RenamexNp(MacPreviousPath(stage), target, unix.RENAME_SWAP); err != nil {
			return err
		}
		return os.Rename(MacPreviousPath(stage), filepath.Join(stage, "failed.app"))
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	return os.Rename(MacPreviousPath(stage), target)
}
