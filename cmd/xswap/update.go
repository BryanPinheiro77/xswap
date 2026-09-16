package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
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
	"runtime"
	"strconv"
	"strings"
	"time"
)

type releaseAsset struct {
	Name string
	URL  string `json:"browser_download_url"`
}
type githubRelease struct {
	Tag               string `json:"tag_name"`
	Draft, Prerelease bool
	Assets            []releaseAsset
}
type updateState struct {
	Repository, Latest string
	Checked            time.Time
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]*[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

const updateCheckInterval = 15 * time.Minute

func newerVersion(latest, current string) bool {
	l := versionPattern.FindStringSubmatch(latest)
	c := versionPattern.FindStringSubmatch(current)
	if l == nil {
		return false
	}
	if c == nil {
		return current == "dev" || strings.Contains(current, "-")
	}
	for i := 1; i <= 3; i++ {
		x, e := strconv.ParseUint(l[i], 10, 64)
		y, f := strconv.ParseUint(c[i], 10, 64)
		if e != nil || f != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return false
}
func (a *App) repository() string {
	var cfg struct{ Repository string }
	data, _ := os.ReadFile(filepath.Join(a.Root, "update.json"))
	_ = json.Unmarshal(data, &cfg)
	if cfg.Repository != "" {
		return cfg.Repository
	}
	return releaseRepo
}
func (a *App) cachedUpdate() updateState {
	var s updateState
	data, _ := os.ReadFile(filepath.Join(a.Root, "update-state.json"))
	_ = json.Unmarshal(data, &s)
	return s
}
func (a *App) download(ctx context.Context, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "XSwap/"+version)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := http.Client{Timeout: 20 * time.Second}
	if a.HTTPClient != nil {
		client = *a.HTTPClient
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 10 {
			return errors.New("unsafe release redirect")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("GitHub request failed; check your connection")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("release download exceeds size limit")
	}
	return data, nil
}
func (a *App) latestRelease(ctx context.Context) (githubRelease, error) {
	repo := a.repository()
	if !repoPattern.MatchString(repo) {
		return githubRelease{}, errors.New("release repository not configured; run xswap update --repo OWNER/xswap --check")
	}
	data, err := a.download(ctx, "https://api.github.com/repos/"+repo+"/releases/latest", 1<<20)
	if err != nil {
		return githubRelease{}, err
	}
	var r githubRelease
	if err = json.Unmarshal(data, &r); err != nil {
		return r, errors.New("invalid GitHub release response")
	}
	if r.Draft || r.Prerelease || !versionPattern.MatchString(r.Tag) {
		return r, errors.New("no valid stable release available")
	}
	err = writeJSON(filepath.Join(a.Root, "update-state.json"), updateState{Repository: repo, Latest: r.Tag, Checked: time.Now()})
	return r, err
}
func (a *App) checkUpdate(ctx context.Context) bool {
	if a.repository() == "" {
		return false
	}
	s := a.cachedUpdate()
	if s.Repository == a.repository() && time.Since(s.Checked) >= 0 && time.Since(s.Checked) < updateCheckInterval {
		return newerVersion(s.Latest, version)
	}
	r, err := a.latestRelease(ctx)
	return err == nil && newerVersion(r.Tag, version)
}
func assetURL(r githubRelease, repo, name string) (string, error) {
	for _, asset := range r.Assets {
		if asset.Name != name {
			continue
		}
		u, err := url.Parse(asset.URL)
		if err == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "/"+repo+"/releases/download/"+r.Tag+"/"+name {
			return asset.URL, nil
		}
	}
	return "", fmt.Errorf("release asset missing or invalid: %s", name)
}
func unpackExecutable(data []byte) ([]byte, error) {
	if runtime.GOOS == "windows" {
		return unpackZipExecutable(data, "xswap.exe")
	}
	return unpackTarExecutable(data, "xswap")
}
func unpackTarExecutable(data []byte, expected string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, 64<<20))
	var binary []byte
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > 32<<20 {
			return nil, errors.New("invalid executable archive")
		}
		if h.Name == "README.md" || h.Name == "LICENSE" {
			continue
		}
		if h.Name != expected || binary != nil {
			return nil, errors.New("invalid executable archive")
		}
		binary, err = io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
	}
	if binary == nil {
		return nil, errors.New("archive has no XSwap executable")
	}
	return binary, nil
}
func unpackZipExecutable(data []byte, expected string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var binary []byte
	for _, entry := range zr.File {
		if entry.Name == "README.md" || entry.Name == "LICENSE" {
			continue
		}
		if entry.Name != expected || binary != nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > 32<<20 {
			return nil, errors.New("invalid executable archive")
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			return nil, openErr
		}
		binary, err = io.ReadAll(io.LimitReader(reader, 32<<20+1))
		closeErr := reader.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(binary) == 0 || len(binary) > 32<<20 {
			return nil, errors.New("invalid executable archive")
		}
	}
	if binary == nil {
		return nil, errors.New("archive has no XSwap executable")
	}
	return binary, nil
}
func validExecutable(data []byte) bool {
	if runtime.GOOS == "darwin" {
		f, err := macho.NewFile(bytes.NewReader(data))
		if err != nil {
			return false
		}
		defer f.Close()
		return (runtime.GOARCH == "arm64" && f.Cpu == macho.CpuArm64) || (runtime.GOARCH == "amd64" && f.Cpu == macho.CpuAmd64)
	}
	if runtime.GOOS == "linux" {
		f, err := elf.NewFile(bytes.NewReader(data))
		if err != nil {
			return false
		}
		defer f.Close()
		return (runtime.GOARCH == "arm64" && f.Machine == elf.EM_AARCH64) || (runtime.GOARCH == "amd64" && f.Machine == elf.EM_X86_64)
	}
	if runtime.GOOS == "windows" {
		f, err := pe.NewFile(bytes.NewReader(data))
		if err != nil {
			return false
		}
		defer f.Close()
		return (runtime.GOARCH == "arm64" && f.Machine == pe.IMAGE_FILE_MACHINE_ARM64) || (runtime.GOARCH == "amd64" && f.Machine == pe.IMAGE_FILE_MACHINE_AMD64)
	}
	return false
}
func releaseArchiveName(tag, goos, goarch string) string {
	extension := ".tar.gz"
	if goos == "windows" {
		extension = ".zip"
	}
	return fmt.Sprintf("xswap_%s_%s_%s%s", tag, goos, goarch, extension)
}
func (a *App) installRelease(ctx context.Context, r githubRelease) error {
	name := releaseArchiveName(r.Tag, runtime.GOOS, runtime.GOARCH)
	archiveURL, err := assetURL(r, a.repository(), name)
	if err != nil {
		return err
	}
	checksumURL, err := assetURL(r, a.repository(), "checksums.txt")
	if err != nil {
		return err
	}
	checks, err := a.download(ctx, checksumURL, 256<<10)
	if err != nil {
		return err
	}
	archive, err := a.download(ctx, archiveURL, 32<<20)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(archive)
	matches := 0
	for _, line := range strings.Split(string(checks), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && strings.TrimPrefix(strings.TrimPrefix(parts[1], "*"), "./") == name {
			if parts[0] != hex.EncodeToString(sum[:]) {
				return errors.New("release checksum mismatch")
			}
			matches++
		}
	}
	if matches != 1 {
		return errors.New("release checksum missing or duplicated")
	}
	binary, err := unpackExecutable(archive)
	if err != nil {
		return err
	}
	if !validExecutable(binary) {
		return errors.New("release executable does not match this platform")
	}
	unlock, err := a.lock(ctx, "update.lock")
	if err != nil {
		return err
	}
	defer unlock()
	if a.Binary == "" {
		return errors.New("installed executable location unavailable")
	}
	// Save the previous executable without touching account data or selection.
	old, err := os.ReadFile(a.Binary)
	if err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(a.Root, "previous-xswap"), old); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(a.Binary), ".xswap-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(binary); err == nil {
		err = tmp.Chmod(0755)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), a.Binary)
}
func (a *App) updateCommand(o Options) (bool, error) {
	if repo := o.Values["repo"]; repo != "" {
		if !repoPattern.MatchString(repo) {
			return false, errors.New("repository must be OWNER/REPO")
		}
		if err := writeJSON(filepath.Join(a.Root, "update.json"), map[string]string{"Repository": repo}); err != nil {
			return false, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r, err := a.latestRelease(ctx)
	if err != nil {
		return false, err
	}
	fmt.Printf("Installed: %s · Latest: %s\n", version, r.Tag)
	if !newerVersion(r.Tag, version) {
		fmt.Println("XSwap is up to date.")
		return false, nil
	}
	if o.Flags["check"] {
		fmt.Println("Update available. Run xswap update to install.")
		return false, nil
	}
	if !o.Flags["yes"] {
		if !interactive() {
			return false, errors.New("use an interactive terminal or --yes to confirm installation")
		}
		fmt.Printf("Install %s from %s? [y/N] ", r.Tag, a.repository())
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			return false, nil
		}
	}
	if err = a.installRelease(ctx, r); err != nil {
		return false, err
	}
	fmt.Println("Updated to", r.Tag, "— restart XSwap to use the new version.")
	return true, nil
}
