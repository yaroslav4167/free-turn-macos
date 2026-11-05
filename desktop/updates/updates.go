// Package updates checks public GitHub releases and downloads verified archives.
// Downloads never replace installed files; confirmed installations are staged
// separately and handed to a user-level worker after the GUI exits.
package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxArchive = 512 << 20

type State struct {
	Repository          string `json:"repository"`
	CurrentVersion      string `json:"currentVersion"`
	CurrentKnown        bool   `json:"currentKnown"`
	LatestVersion       string `json:"latestVersion"`
	Available           bool   `json:"available"`
	AssetName           string `json:"assetName"`
	ReleaseURL          string `json:"releaseUrl"`
	CheckedAt           string `json:"checkedAt"`
	Busy                string `json:"busy"`
	Phase               string `json:"phase"`
	BytesPerSecond      int64  `json:"bytesPerSecond"`
	RemainingSeconds    int64  `json:"remainingSeconds"`
	Error               string `json:"error"`
	DownloadedPath      string `json:"downloadedPath"`
	Received            int64  `json:"received"`
	Total               int64  `json:"total"`
	InstallationMessage string `json:"installationMessage"`
	BackupPath          string `json:"backupPath"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}
type release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

type Manager struct {
	mu              sync.Mutex
	state           State
	platform        string
	arch            string
	client          *http.Client
	apiURL          string
	archive         asset
	checksum        asset
	repository      string
	downloadStarted time.Time
}

func New(version, arch, repository string) *Manager {
	_, known := parseVersion(version)
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_][A-Za-z0-9_.-]*$`).MatchString(repository) {
		repository = ""
	}
	return &Manager{
		state: State{CurrentVersion: version, CurrentKnown: known, Repository: repository}, arch: arch, repository: repository,
		apiURL: "https://api.github.com/repos/" + repository + "/releases/latest",
		client: &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("слишком много перенаправлений")
			}
			if !trustedURL(req.URL.String()) {
				return errors.New("небезопасное перенаправление загрузки")
			}
			return nil
		}},
	}
}

func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.state
	state.BytesPerSecond, state.RemainingSeconds = 0, -1
	if state.Phase == "downloading" && !m.downloadStarted.IsZero() {
		state.BytesPerSecond, state.RemainingSeconds = transferEstimate(state.Received, state.Total, time.Since(m.downloadStarted))
	}
	return state
}

func transferEstimate(received, total int64, elapsed time.Duration) (int64, int64) {
	if received <= 0 || elapsed < 200*time.Millisecond {
		return 0, -1
	}
	rate := int64(float64(received) / elapsed.Seconds())
	if rate <= 0 || total <= 0 {
		return rate, -1
	}
	remaining := max(int64(0), total-received)
	return rate, (remaining + rate - 1) / rate
}

func (m *Manager) setPhase(phase string) {
	m.mu.Lock()
	m.state.Phase = phase
	m.mu.Unlock()
}

func (m *Manager) begin(operation string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Busy != "" {
		return errors.New("проверка или скачивание уже выполняется")
	}
	m.state.Busy, m.state.Error = operation, ""
	m.state.Phase = operation
	m.state.Received, m.state.Total = 0, 0
	m.downloadStarted = time.Time{}
	return nil
}
func (m *Manager) finish(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	operation := m.state.Busy
	m.state.Busy = ""
	if err != nil {
		m.state.Error = err.Error()
		m.state.Phase = "error"
	} else {
		switch operation {
		case "downloading":
			m.state.Phase = "downloaded"
		case "preparing":
			m.state.Phase = "ready"
		default:
			m.state.Phase = ""
		}
	}
}

func (m *Manager) Check(ctx context.Context) (err error) {
	if err = m.begin("checking"); err != nil {
		return err
	}
	defer func() { m.finish(err) }()
	if m.repository == "" {
		return errors.New("репозиторий обновлений не задан при сборке")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, err := m.get(ctx, m.apiURL)
	if err != nil {
		return err
	}
	defer body.Close()
	var latest release
	if err = json.NewDecoder(io.LimitReader(body, 2<<20)).Decode(&latest); err != nil {
		return fmt.Errorf("ответ GitHub: %w", err)
	}
	if _, ok := parseVersion(latest.Tag); !ok || latest.Draft || latest.Prerelease {
		return errors.New("релиз не является стабильной версией")
	}
	var archive, checksum asset
	expected := m.archiveName(latest.Tag)
	for _, a := range latest.Assets {
		if a.Name == expected {
			archive = a
		}
		if a.Name == "checksums.txt" {
			checksum = a
		}
	}
	if archive.URL == "" || checksum.URL == "" {
		return errors.New("в последнем релизе нет архива для этой архитектуры или checksums.txt")
	}
	if !m.trustedAsset(archive.URL) || !m.trustedAsset(checksum.URL) || archive.Size <= 0 || archive.Size > maxArchive {
		return errors.New("небезопасный адрес или недопустимый размер файла релиза")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.archive, m.checksum = archive, checksum
	if m.state.LatestVersion != latest.Tag {
		m.state.DownloadedPath = ""
	}
	m.state.LatestVersion, m.state.AssetName = latest.Tag, archive.Name
	m.state.ReleaseURL = "https://github.com/" + m.repository + "/releases/tag/" + url.PathEscape(latest.Tag)
	m.state.Available = !m.state.CurrentKnown || newer(latest.Tag, m.state.CurrentVersion)
	m.state.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	return nil
}

// Download takes only a destination and the previously checked tag, never a URL
// from the webview. It publishes the file atomically, only after checksum success.
func (m *Manager) Download(ctx context.Context, destination, tag string) (err error) {
	if err = m.begin("downloading"); err != nil {
		return err
	}
	defer func() { m.finish(err) }()
	return m.download(ctx, destination, tag)
}

// Restart reports the handoff while the separate installer worker starts up.
// Actual file replacement happens after the old GUI has exited.
func (m *Manager) Restart(start func() error) (err error) {
	if err = m.begin("restarting"); err != nil {
		return err
	}
	defer func() { m.finish(err) }()
	return start()
}

func (m *Manager) download(ctx context.Context, destination, tag string) (err error) {
	m.mu.Lock()
	archive, checksum := m.archive, m.checksum
	valid := m.state.Available && tag == m.state.LatestVersion && archive.URL != ""
	m.state.Received, m.state.Total = 0, archive.Size
	m.mu.Unlock()
	if !valid {
		return errors.New("сначала проверьте доступное обновление")
	}
	if !filepath.IsAbs(destination) {
		return errors.New("выберите абсолютный путь сохранения")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	checkCtx, checkCancel := context.WithTimeout(ctx, 30*time.Second)
	m.setPhase("checksum")
	expected, err := m.expectedChecksum(checkCtx, checksum.URL, archive.Name)
	checkCancel()
	if err != nil {
		return err
	}
	m.setPhase("connecting")
	body, err := m.get(ctx, archive.URL)
	if err != nil {
		return err
	}
	defer body.Close()
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".freeturn-download-*")
	if err != nil {
		return err
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()
	hash := sha256.New()
	m.mu.Lock()
	m.downloadStarted = time.Now()
	m.state.Phase = "downloading"
	m.mu.Unlock()
	n, err := io.Copy(io.MultiWriter(tmp, hash, progress{m}), io.LimitReader(body, archive.Size+1))
	if err != nil {
		return err
	}
	if n != archive.Size {
		return errors.New("размер скачанного архива не совпадает с релизом")
	}
	m.setPhase("verifying")
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return errors.New("SHA-256 не совпадает: файл не сохранён")
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), destination); err != nil {
		return err
	}
	m.mu.Lock()
	m.state.DownloadedPath = destination
	m.mu.Unlock()
	return nil
}

type progress struct{ manager *Manager }

func (p progress) Write(data []byte) (int, error) {
	p.manager.mu.Lock()
	p.manager.state.Received += int64(len(data))
	p.manager.mu.Unlock()
	return len(data), nil
}

func (m *Manager) get(ctx context.Context, address string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FreeTurn-Desktop-updater")
	req.Header.Set("Accept", "application/vnd.github+json")
	response, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub недоступен: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		switch response.StatusCode {
		case 404:
			return nil, errors.New("опубликованный релиз или файл обновления не найден")
		case 403, 429:
			return nil, errors.New("GitHub ограничил частоту запросов; попробуйте позже")
		default:
			return nil, fmt.Errorf("GitHub: HTTP %d", response.StatusCode)
		}
	}
	return response.Body, nil
}

func (m *Manager) expectedChecksum(ctx context.Context, address, name string) (string, error) {
	body, err := m.get(ctx, address)
	if err != nil {
		return "", err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, (64<<10)+1))
	if err != nil {
		return "", err
	}
	if len(data) > 64<<10 {
		return "", errors.New("слишком большой файл checksums.txt")
	}
	found := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size || found != "" {
			return "", errors.New("некорректная SHA-256 в checksums.txt")
		}
		found = strings.ToLower(fields[0])
	}
	if found == "" {
		return "", errors.New("для архива нет SHA-256 в checksums.txt")
	}
	return found, nil
}

func trustedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	return u.Hostname() == "github.com" || u.Hostname() == "release-assets.githubusercontent.com" || u.Hostname() == "objects.githubusercontent.com"
}
func (m *Manager) trustedAsset(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && trustedURL(raw) && u.Hostname() == "github.com" && strings.HasPrefix(u.Path, "/"+m.repository+"/releases/download/")
}

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:\.(0|[1-9][0-9]*))?$`)

func parseVersion(value string) ([3]uint64, bool) {
	var parts [3]uint64
	matches := versionPattern.FindStringSubmatch(value)
	if matches == nil {
		return parts, false
	}
	for i := range parts {
		if matches[i+1] == "" {
			continue
		}
		number, err := strconv.ParseUint(matches[i+1], 10, 64)
		if err != nil {
			return parts, false
		}
		parts[i] = number
	}
	return parts, true
}
func newer(latest, current string) bool {
	a, _ := parseVersion(latest)
	b, _ := parseVersion(current)
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// NewForPlatform selects native archive naming without changing legacy Linux callers.
func NewForPlatform(version, arch, repository, platform string) *Manager {
	m := New(version, arch, repository)
	m.platform = platform
	return m
}
func (m *Manager) archiveName(version string) string {
	if m.platform == "darwin" {
		return "FreeTurn-macOS-" + m.arch + "-" + version + ".zip"
	}
	return "FreeTurn-Linux-" + m.arch + "-" + version + ".tar.gz"
}
