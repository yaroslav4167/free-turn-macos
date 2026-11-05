package updates

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var binaryNames = []string{"freeturn-desktop", "freeturn-vpn-helper"}

const InstallProtocolMarker = "FREETURN_UPDATE_PROTOCOL_V1"

type installPlan struct {
	Version  string            `json:"version"`
	Hashes   map[string]string `json:"hashes"`
	Previous map[string]bool   `json:"previous"`
}

// PrepareInstall downloads and stages only the two expected ELF binaries. The
// installed files remain untouched until the separate worker applies the plan.
func (m *Manager) PrepareInstall(ctx context.Context, targetDir string) (stage string, err error) {
	if err = m.begin("preparing"); err != nil {
		return "", err
	}
	defer func() { m.finish(err) }()
	state := m.State()
	if !state.Available {
		return "", errors.New("сначала проверьте доступное обновление")
	}
	if !filepath.IsAbs(targetDir) {
		return "", errors.New("недопустимый каталог установки")
	}
	if err = os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	info, err := os.Lstat(targetDir)
	if err != nil || !info.IsDir() {
		return "", errors.New("каталог установки не должен быть символической ссылкой")
	}
	stage, err = os.MkdirTemp(targetDir, ".freeturn-update-")
	if err != nil {
		return "", err
	}
	ownedStage := stage
	defer func() {
		if err != nil {
			_ = os.RemoveAll(ownedStage)
		}
		m.mu.Lock()
		if m.state.DownloadedPath == filepath.Join(ownedStage, "archive.tar.gz") {
			m.state.DownloadedPath = ""
		}
		m.mu.Unlock()
	}()
	archive := filepath.Join(stage, "archive.tar.gz")
	if err = m.download(ctx, archive, state.LatestVersion); err != nil {
		return "", err
	}
	m.setPhase("extracting")
	if err = stageArchive(ctx, archive, stage, state.LatestVersion, m.arch); err != nil {
		return "", err
	}
	if err = os.Remove(archive); err != nil {
		return "", err
	}
	return stage, nil
}

func stageArchive(ctx context.Context, archive, stage, version, arch string) error {
	input, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer input.Close()
	gz, err := gzip.NewReader(contextReader{ctx, input})
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	if err := os.Mkdir(filepath.Join(stage, "new"), 0o700); err != nil {
		return err
	}
	plan := installPlan{Version: version, Hashes: map[string]string{}, Previous: map[string]bool{}}
	var total int64
	for entries := 0; ; entries++ {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		clean := path.Clean(header.Name)
		if entries >= 2048 || strings.ContainsAny(header.Name, "\\\x00") || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return errors.New("небезопасный путь или слишком много файлов в архиве")
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
			return errors.New("ссылки и специальные файлы в архиве обновления запрещены")
		}
		total += header.Size
		if header.Size < 0 || header.Size > maxArchive || total > 1<<30 {
			return errors.New("слишком большой распакованный архив")
		}
		if clean != binaryNames[0] && clean != binaryNames[1] {
			continue
		}
		if header.Typeflag == tar.TypeDir || header.Size == 0 || plan.Hashes[clean] != "" {
			return errors.New("неверный или повторяющийся бинарник в архиве")
		}
		target := filepath.Join(stage, "new", clean)
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(file, hash), reader)
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
			return err
		}
		if err := validateBinary(target, arch); err != nil {
			return fmt.Errorf("%s: %w", clean, err)
		}
		if clean == binaryNames[0] && !supportsInstallProtocol(target) {
			return errors.New("эта версия не поддерживает безопасный перезапуск; используйте ручную установку архива")
		}
		plan.Hashes[clean] = hex.EncodeToString(hash.Sum(nil))
	}
	// Consume the gzip trailer as well; a truncated/corrupt stream must not install.
	if n, err := io.Copy(io.Discard, io.LimitReader(gz, (1<<20)+1)); err != nil {
		return err
	} else if n > 1<<20 {
		return errors.New("слишком много данных после конца tar-архива")
	}
	for _, name := range binaryNames {
		if plan.Hashes[name] == "" {
			return fmt.Errorf("в архиве отсутствует %s", name)
		}
	}
	return writePlan(stage, plan)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func validateBinary(filename, arch string) error {
	machines := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64, "386": elf.EM_386, "arm": elf.EM_ARM}
	machine, ok := machines[arch]
	if !ok {
		return errors.New("архитектура обновления не поддерживается")
	}
	binary, err := elf.Open(filename)
	if err != nil {
		return errors.New("файл не является ELF-бинарником")
	}
	defer binary.Close()
	if binary.Machine != machine || (binary.Type != elf.ET_EXEC && binary.Type != elf.ET_DYN) {
		return errors.New("бинарник предназначен для другой архитектуры или не исполняемый")
	}
	return nil
}

func supportsInstallProtocol(filename string) bool {
	return supportsMarker(filename, InstallProtocolMarker)
}

func supportsMarker(filename, marker string) bool {
	file, err := os.Open(filename)
	if err != nil {
		return false
	}
	defer file.Close()
	buffer := make([]byte, 65536)
	var tail []byte
	for {
		n, err := file.Read(buffer)
		data := append(tail, buffer[:n]...)
		if bytes.Contains(data, []byte(marker)) {
			return true
		}
		if err != nil {
			return false
		}
		if len(data) >= len(marker) {
			tail = append([]byte(nil), data[len(data)-len(marker)+1:]...)
		}
	}
}

func writePlan(stage string, plan installPlan) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(stage, ".plan-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(stage, "plan.json"))
}

func readPlan(stage, target string) (installPlan, error) {
	var plan installPlan
	if !filepath.IsAbs(stage) || filepath.Dir(stage) != filepath.Clean(target) || !strings.HasPrefix(filepath.Base(stage), ".freeturn-update-") {
		return plan, errors.New("недопустимый каталог обновления")
	}
	info, err := os.Lstat(stage)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return plan, errors.New("небезопасный каталог обновления")
	}
	file, err := os.Open(filepath.Join(stage, "plan.json"))
	if err != nil {
		return plan, err
	}
	defer file.Close()
	if err := json.NewDecoder(io.LimitReader(file, 8192)).Decode(&plan); err != nil {
		return plan, err
	}
	if plan.Hashes == nil || plan.Previous == nil {
		return plan, errors.New("неполный план обновления")
	}
	if _, valid := parseVersion(plan.Version); !valid {
		return plan, errors.New("недопустимая версия обновления")
	}
	return plan, nil
}

func ValidatePrepared(stage, target, arch string) error {
	plan, err := readPlan(stage, target)
	if err != nil {
		return err
	}
	for _, name := range binaryNames {
		filename := filepath.Join(stage, "new", name)
		info, err := os.Lstat(filename)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("бинарник обновления отсутствует или является ссылкой")
		}
		file, err := os.Open(filename)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, file)
		file.Close()
		if err != nil {
			return err
		}
		if plan.Hashes[name] == "" || hex.EncodeToString(hash.Sum(nil)) != plan.Hashes[name] {
			return errors.New("подготовленный бинарник изменён после проверки")
		}
		if err := validateBinary(filename, arch); err != nil {
			return err
		}
		if name == binaryNames[0] && !supportsInstallProtocol(filename) {
			return errors.New("клиент не поддерживает безопасный перезапуск")
		}
	}
	return nil
}

// InstallPrepared backs up BOTH old files before atomically replacing either
// one. On any replacement error it restores the previous pair automatically.
func InstallPrepared(stage, target, arch string) error {
	return installPrepared(stage, target, arch, os.Rename)
}

func installPrepared(stage, target, arch string, replace func(string, string) error) error {
	if err := ValidatePrepared(stage, target, arch); err != nil {
		return err
	}
	plan, err := readPlan(stage, target)
	if err != nil {
		return err
	}
	previous := filepath.Join(stage, "previous")
	if err := os.Mkdir(previous, 0o700); err != nil {
		return err
	}
	for _, name := range binaryNames {
		source := filepath.Join(target, name)
		info, err := os.Lstat(source)
		if os.IsNotExist(err) {
			plan.Previous[name] = false
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("установленный бинарник не должен быть ссылкой или каталогом")
		}
		if err := copyAtomic(source, filepath.Join(previous, name)); err != nil {
			return err
		}
		plan.Previous[name] = true
	}
	if err := writePlan(stage, plan); err != nil {
		return err
	}
	replaced := false
	for _, name := range binaryNames {
		source := filepath.Join(stage, "new", name)
		if err := os.Chmod(source, 0o755); err != nil {
			if replaced {
				return errors.Join(err, RestorePrevious(stage, target))
			}
			return err
		}
		if err := replace(source, filepath.Join(target, name)); err != nil {
			return errors.Join(err, RestorePrevious(stage, target))
		}
		replaced = true
	}
	return nil
}

// RestorePrevious preserves the backup so a failed restart can be retried.
func RestorePrevious(stage, target string) error {
	plan, err := readPlan(stage, target)
	if err != nil {
		return err
	}
	var failures []error
	for _, name := range binaryNames {
		existed, recorded := plan.Previous[name]
		if !recorded {
			return errors.New("неполная запись резервной копии")
		}
		destination := filepath.Join(target, name)
		if existed {
			if err := copyAtomic(filepath.Join(stage, "previous", name), destination); err != nil {
				failures = append(failures, err)
			}
		} else if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func copyAtomic(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.CreateTemp(filepath.Dir(destination), ".freeturn-binary-*")
	if err != nil {
		return err
	}
	defer func() { output.Close(); os.Remove(output.Name()) }()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Chmod(0o755); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	return os.Rename(output.Name(), destination)
}
