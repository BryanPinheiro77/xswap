package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fakeTransport func(*http.Request) (*http.Response, error)

func (f fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testArchive(t *testing.T, name string, kind byte, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tr := tar.NewWriter(gz)
	if err := tr.WriteHeader(&tar.Header{Name: name, Typeflag: kind, Size: int64(len(data)), Mode: 0755}); err != nil {
		t.Fatal(err)
	}
	if kind == tar.TypeReg {
		if _, err := tr.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	tr.Close()
	gz.Close()
	return b.Bytes()
}

type zipEntry struct {
	name string
	data []byte
}

func testZipArchive(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, entry := range entries {
		writer, err := zw.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = writer.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestUpdateVisibilityAndVersions(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{{"v0.10.0", "v0.9.0", true}, {"v1.0.0", "v1.0.0", false}, {"v0.1.0", "dev", true}, {"v1.0.0-rc1", "dev", false}, {"v1.0.0", "v2.0.0", false}} {
		if newerVersion(tc.latest, tc.current) != tc.want {
			t.Errorf("comparison: %+v", tc)
		}
	}
	p := Panel{}
	if strings.Contains(strings.Join(p.menu(), " "), "Update version") {
		t.Fatal("update shown without new release")
	}
	p.UpdateAvailable = true
	if !strings.Contains(strings.Join(p.menu(), " "), "Update version") {
		t.Fatal("available update hidden")
	}
	p.Mode = "home"
	p.MenuCursor = 6
	action, err := p.key(fixture(t), []string{"default"}, "enter")
	if err != nil || action != "" || p.Mode != "confirm-update" {
		t.Fatalf("update selection did not request confirmation: %s %s %v", p.Mode, action, err)
	}
	action, err = p.key(fixture(t), []string{"default"}, "y")
	if err != nil || action != "update" {
		t.Fatalf("confirmed update selection: %s %v", action, err)
	}
	p.Mode = "confirm-update"
	action, err = p.key(fixture(t), []string{"default"}, "esc")
	if err != nil || action != "" || p.Mode != "home" {
		t.Fatalf("cancelled update selection: %s %s %v", p.Mode, action, err)
	}
	a := fixture(t)
	if a.checkUpdate(context.Background()) {
		t.Fatal("unconfigured repository has update")
	}
}

func TestHomebrewManagedInstallUsesPackageUpdates(t *testing.T) {
	a := fixture(t)
	a.PackageManager = "homebrew"
	if err := writeJSON(filepath.Join(a.Root, "update.json"), map[string]string{"Repository": "owner/xswap"}); err != nil {
		t.Fatal(err)
	}
	requests := 0
	a.HTTPClient = &http.Client{Transport: fakeTransport(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v999.0.0"}`))}, nil
	})}
	if !a.checkUpdate(context.Background()) || requests != 1 {
		t.Fatal("package-managed install did not detect a GitHub release")
	}
	commands := []string{}
	a.CommandRunner = func(_ context.Context, name string, args ...string) error {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		return nil
	}
	installed, err := a.updateCommand(Options{Flags: map[string]bool{"check": true}})
	if installed || err != nil || len(commands) != 0 {
		t.Fatal("Homebrew check attempted installation", installed, err, commands)
	}
	installed, err = a.updateCommand(Options{Flags: map[string]bool{"yes": true}})
	if !installed || err != nil || strings.Join(commands, ",") != "brew update,brew upgrade xswap" {
		t.Fatal("package-managed update was not routed through Homebrew", installed, err, commands)
	}
}

func TestHomebrewUpdatePreservesCommandFailures(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		failAt, calls int
	}{
		{"refresh", "brew update failed", 1, 1},
		{"upgrade", "brew upgrade xswap failed", 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := fixture(t)
			a.PackageManager = "homebrew"
			if err := writeJSON(filepath.Join(a.Root, "update.json"), map[string]string{"Repository": "owner/xswap"}); err != nil {
				t.Fatal(err)
			}
			a.HTTPClient = &http.Client{Transport: fakeTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v999.0.0"}`))}, nil
			})}
			commands := 0
			a.CommandRunner = func(_ context.Context, name string, args ...string) error {
				commands++
				if commands == tc.failAt {
					return errors.New("package manager unavailable")
				}
				return nil
			}
			installed, err := a.updateCommand(Options{Flags: map[string]bool{"yes": true}})
			if installed || err == nil || !strings.Contains(err.Error(), tc.message) || commands != tc.calls {
				t.Fatal("Homebrew command failure was not preserved", installed, err, commands)
			}
		})
	}
}

func TestReleaseChecksCacheAndRejectDrafts(t *testing.T) {
	a := fixture(t)
	if err := writeJSON(filepath.Join(a.Root, "update.json"), map[string]string{"Repository": "owner/xswap"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	response := `{"tag_name":"v999.0.0","draft":false,"prerelease":false}`
	a.HTTPClient = &http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://api.github.com/repos/owner/xswap/releases/latest" {
			t.Error("wrong API URL")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
	})}
	if !a.checkUpdate(context.Background()) || !a.checkUpdate(context.Background()) || calls != 1 {
		t.Fatal("new release/cache detection failed")
	}
	if err := writeJSON(filepath.Join(a.Root, "update-state.json"), updateState{Repository: "owner/xswap", Latest: "v0.1.0", Checked: time.Now().Add(-updateCheckInterval - time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if !a.checkUpdate(context.Background()) || calls != 2 {
		t.Fatal("stale update cache was not refreshed")
	}
	response = `{"tag_name":"v999.0.0","draft":true}`
	if _, err := a.latestRelease(context.Background()); err == nil {
		t.Fatal("accepted draft")
	}
	response = `{"tag_name":"v999.0.0","prerelease":true}`
	if _, err := a.latestRelease(context.Background()); err == nil {
		t.Fatal("accepted prerelease")
	}
}
func TestArchiveRejectsUnsafeEntries(t *testing.T) {
	for _, name := range []string{"../xswap", "/xswap", "other"} {
		if _, err := unpackExecutable(testArchive(t, name, tar.TypeReg, []byte("bad"))); err == nil {
			t.Fatal("accepted", name)
		}
	}
	if _, err := unpackExecutable(testArchive(t, "xswap", tar.TypeSymlink, nil)); err == nil {
		t.Fatal("accepted symlink")
	}
	for _, name := range []string{"../xswap.exe", "/xswap.exe", "other.exe"} {
		archive := testZipArchive(t, zipEntry{name, []byte("bad")})
		if _, err := unpackZipExecutable(archive, "xswap.exe"); err == nil {
			t.Fatal("accepted zip entry", name)
		}
	}
	duplicate := testZipArchive(t,
		zipEntry{"xswap.exe", []byte("first")},
		zipEntry{"xswap.exe", []byte("second")},
	)
	if _, err := unpackZipExecutable(duplicate, "xswap.exe"); err == nil {
		t.Fatal("accepted duplicate Windows executable")
	}
	valid := testZipArchive(t,
		zipEntry{"xswap.exe", []byte("binary")},
		zipEntry{"README.md", []byte("docs")},
	)
	if binary, err := unpackZipExecutable(valid, "xswap.exe"); err != nil || string(binary) != "binary" {
		t.Fatal("valid Windows archive rejected", err)
	}
	if got := releaseArchiveName("v1.2.3", "windows", "amd64"); got != "xswap_v1.2.3_windows_amd64.zip" {
		t.Fatal("wrong Windows asset name", got)
	}
}

func TestReleaseCheckFailuresHideUpdate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		fail   bool
	}{
		{"offline", 0, "", true},
		{"not published", 404, "", false},
		{"rate limited", 403, "", false},
		{"invalid JSON", 200, "{", false},
		{"invalid version", 200, `{"tag_name":"invalid"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := fixture(t)
			if err := writeJSON(filepath.Join(a.Root, "update.json"), map[string]string{"Repository": "owner/xswap"}); err != nil {
				t.Fatal(err)
			}
			a.HTTPClient = &http.Client{Transport: fakeTransport(func(*http.Request) (*http.Response, error) {
				if tc.fail {
					return nil, errors.New("offline")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			if a.checkUpdate(context.Background()) {
				t.Fatal("failed check advertised an update")
			}
		})
	}
}
func TestReleaseInstallChecksumsAndAtomicReplacement(t *testing.T) {
	a := fixture(t)
	a.Binary = filepath.Join(t.TempDir(), "xswap")
	os.WriteFile(a.Binary, []byte("old"), 0755)
	if err := writeJSON(filepath.Join(a.Root, "update.json"), map[string]string{"Repository": "owner/xswap"}); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	archive := testArchive(t, "xswap", tar.TypeReg, binary)
	name := fmt.Sprintf("xswap_v1.0.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	base := "https://github.com/owner/xswap/releases/download/v1.0.0/"
	release := githubRelease{Tag: "v1.0.0", Assets: []releaseAsset{{Name: name, URL: base + name}, {Name: "checksums.txt", URL: base + "checksums.txt"}}}
	corrupt := true
	a.HTTPClient = &http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		data := archive
		if r.URL.Host == "api.github.com" {
			data, _ = json.Marshal(release)
		}
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			sum := sha256.Sum256(archive)
			if corrupt {
				sum[0] ^= 1
			}
			data = []byte(fmt.Sprintf("%x  ./%s\n", sum, name))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: http.Header{}}, nil
	})}
	if err := a.installRelease(context.Background(), release); err == nil {
		t.Fatal("accepted checksum mismatch")
	}
	old, _ := os.ReadFile(a.Binary)
	if string(old) != "old" {
		t.Fatal("modified executable on failure")
	}
	corrupt = false
	if err := a.installRelease(context.Background(), release); err != nil {
		t.Fatal(err)
	}
	installed, _ := os.ReadFile(a.Binary)
	if !bytes.Equal(installed, binary) {
		t.Fatal("wrong installed executable")
	}
	backup, _ := os.ReadFile(filepath.Join(a.Root, "previous-xswap"))
	if string(backup) != "old" {
		t.Fatal("missing backup")
	}
	installedOK, err := a.updateCommand(Options{Flags: map[string]bool{"yes": true}})
	if err != nil || !installedOK {
		t.Fatal("confirmed command failed", installedOK, err)
	}
	release.Assets[0].URL = "https://example.com/evil"
	if _, err := assetURL(release, "owner/xswap", name); err == nil {
		t.Fatal("accepted foreign asset")
	}
}

func TestUpdateCommandChecksWithoutInstalling(t *testing.T) {
	a := fixture(t)
	t.Setenv("TERM", "dumb")
	calls := 0
	a.HTTPClient = &http.Client{Transport: fakeTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v999.0.0"}`))}, nil
	})}
	if installed, err := a.updateCommand(Options{Flags: map[string]bool{"check": true}, Values: map[string]string{"repo": "owner/xswap"}}); err != nil || installed {
		t.Fatal(installed, err)
	}
	if calls != 1 || a.repository() != "owner/xswap" {
		t.Fatal("check downloaded assets or lost repository")
	}
	if installed, err := a.updateCommand(Options{}); err == nil || installed {
		t.Fatal("unconfirmed noninteractive install", installed, err)
	}
	if _, err := a.updateCommand(Options{Values: map[string]string{"repo": "../invalid"}}); err == nil {
		t.Fatal("accepted invalid repository")
	}
	if a.repository() != "owner/xswap" {
		t.Fatal("invalid option changed repository")
	}
}

func TestDownloadLimitsAndInvalidExecutables(t *testing.T) {
	a := fixture(t)
	a.HTTPClient = &http.Client{Transport: fakeTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("too much"))}, nil
	})}
	if _, err := a.download(context.Background(), "https://github.com/owner/xswap", 3); err == nil {
		t.Fatal("accepted oversized response")
	}
	if validExecutable([]byte("not executable")) {
		t.Fatal("accepted invalid executable")
	}
	if _, err := unpackExecutable([]byte("not gzip")); err == nil {
		t.Fatal("accepted invalid gzip")
	}
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tr := tar.NewWriter(gz)
	for i := 0; i < 2; i++ {
		if err := tr.WriteHeader(&tar.Header{Name: "xswap", Typeflag: tar.TypeReg, Size: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := tr.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	tr.Close()
	gz.Close()
	if _, err := unpackExecutable(b.Bytes()); err == nil {
		t.Fatal("accepted duplicate executable")
	}
}
