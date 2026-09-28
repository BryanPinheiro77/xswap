package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const serviceLabel = "com.bryanpinheiro77.xswap.auto"
const systemdUnit = "xswap-auto.service"

type serviceSpec struct {
	Platform, Path, Root, Binary, LogPath string
	Owner                                 string
}

func newServiceSpec(platform, home, root, binary string) (serviceSpec, error) {
	if platform != "darwin" && platform != "linux" {
		return serviceSpec{}, errors.New("supervised services are supported on macOS and Linux only")
	}
	if !filepath.IsAbs(home) || !filepath.IsAbs(root) {
		return serviceSpec{}, errors.New("service home and manager directory must be absolute paths")
	}
	s := serviceSpec{Platform: platform, Root: filepath.Clean(root), Binary: binary}
	s.Owner = base64.RawURLEncoding.EncodeToString([]byte(s.Root))
	if platform == "darwin" {
		s.Path = filepath.Join(home, "Library", "LaunchAgents", serviceLabel+".plist")
		s.LogPath = filepath.Join(s.Root, "logs", "service.log")
	} else {
		s.Path = filepath.Join(home, ".config", "systemd", "user", systemdUnit)
	}
	return s, nil
}

func (a *App) serviceSpec() (serviceSpec, error) {
	return newServiceSpec(runtime.GOOS, filepath.Dir(a.DefaultHome), a.Root, stableServiceExecutable(a.Binary))
}

func stableServiceExecutable(binary string) string {
	path, err := exec.LookPath("xswap")
	if err != nil {
		return binary
	}
	candidate, candidateErr := os.Stat(path)
	current, currentErr := os.Stat(binary)
	if candidateErr != nil || currentErr != nil || !os.SameFile(candidate, current) {
		return binary
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return binary
	}
	return absolute
}

func (s serviceSpec) marker() string {
	if s.Platform == "darwin" {
		return "<!-- Managed by XSwap; root=" + s.Owner + " -->"
	}
	return "# Managed by XSwap; root=" + s.Owner
}

func (s serviceSpec) definition() ([]byte, error) {
	if !filepath.IsAbs(s.Binary) || strings.ContainsAny(s.Binary, "\r\n\x00") {
		return nil, errors.New("service executable must be an absolute path without control characters")
	}
	if strings.ContainsAny(s.Root, "\r\n\x00") {
		return nil, errors.New("manager directory contains unsupported control characters")
	}
	if s.Platform == "darwin" {
		return []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" + s.marker() + "\n" +
			"<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n" +
			"<plist version=\"1.0\"><dict>\n" +
			"<key>Label</key><string>" + serviceLabel + "</string>\n" +
			"<key>ProgramArguments</key><array><string>" + xmlText(s.Binary) + "</string><string>__daemon</string></array>\n" +
			"<key>EnvironmentVariables</key><dict><key>CODEX_SWAP_HOME</key><string>" + xmlText(s.Root) + "</string></dict>\n" +
			"<key>RunAtLoad</key><true/>\n" +
			"<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>\n" +
			"<key>ThrottleInterval</key><integer>30</integer>\n" +
			"<key>StandardOutPath</key><string>" + xmlText(s.LogPath) + "</string>\n" +
			"<key>StandardErrorPath</key><string>" + xmlText(s.LogPath) + "</string>\n" +
			"</dict></plist>\n"), nil
	}
	return []byte(s.marker() + "\n" +
		"[Unit]\nDescription=XSwap auto-switch monitor\nStartLimitIntervalSec=300\nStartLimitBurst=5\n\n" +
		"[Service]\nType=simple\nExecStart=" + systemdQuote(s.Binary) + " __daemon\n" +
		"Environment=" + systemdEnvironment("CODEX_SWAP_HOME="+s.Root) + "\n" +
		"Restart=on-failure\nRestartSec=30s\n\n" +
		"[Install]\nWantedBy=default.target\n"), nil
}

func xmlText(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(value)
}

func systemdQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`).Replace(value) + `"`
}

func systemdEnvironment(value string) string {
	// Environment= performs specifier expansion, but treats '$' literally.
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(value) + `"`
}

func (s serviceSpec) readOwned() ([]byte, bool, error) {
	info, err := os.Lstat(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("refusing to use non-regular service file %s", s.Path)
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, false, err
	}
	prefix := s.marker() + "\n"
	if s.Platform == "darwin" {
		prefix = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" + prefix
	}
	if !bytes.HasPrefix(data, []byte(prefix)) {
		return nil, false, fmt.Errorf("refusing to change a service file not owned by this XSwap manager: %s", s.Path)
	}
	return data, true, nil
}

func (a *App) serviceInstalled() (bool, error) {
	if runtime.GOOS == "windows" {
		return false, nil
	}
	s, err := a.serviceSpec()
	if err != nil {
		return false, err
	}
	_, installed, err := s.readOwned()
	return installed, err
}

func (a *App) runServiceCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if a.ServiceRunner != nil {
		return a.ServiceRunner(ctx, name, args...)
	}
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 2048 {
			message = message[:2048]
		}
		if message != "" {
			return string(output), fmt.Errorf("%s: %w: %s", name, err, message)
		}
	}
	return string(output), err
}

func (s serviceSpec) launchTarget() string {
	return "gui/" + strconv.Itoa(os.Getuid()) + "/" + serviceLabel
}

func (a *App) serviceManagerLoaded(s serviceSpec) bool {
	if s.Platform == "darwin" {
		_, err := a.runServiceCommand("launchctl", "print", s.launchTarget())
		return err == nil
	}
	output, err := a.runServiceCommand("systemctl", "--user", "is-enabled", systemdUnit)
	return err == nil && strings.TrimSpace(output) == "enabled"
}

func (a *App) registerService(s serviceSpec) error {
	if s.Platform == "darwin" {
		_, err := a.runServiceCommand("launchctl", "bootstrap", "gui/"+strconv.Itoa(os.Getuid()), s.Path)
		return err
	}
	if _, err := a.runServiceCommand("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	_, err := a.runServiceCommand("systemctl", "--user", "enable", systemdUnit)
	return err
}

func (a *App) unregisterService(s serviceSpec) error {
	if s.Platform == "darwin" {
		if !a.serviceManagerLoaded(s) {
			return nil
		}
		_, err := a.runServiceCommand("launchctl", "bootout", s.launchTarget())
		return err
	}
	_, err := a.runServiceCommand("systemctl", "--user", "disable", "--now", systemdUnit)
	return err
}

func (a *App) startInstalledService() error {
	s, err := a.serviceSpec()
	if err != nil {
		return err
	}
	if s.Platform == "darwin" {
		if !a.serviceManagerLoaded(s) {
			return a.registerService(s)
		}
		_, err = a.runServiceCommand("launchctl", "kickstart", "-k", s.launchTarget())
		return err
	}
	_, err = a.runServiceCommand("systemctl", "--user", "start", systemdUnit)
	return err
}

func (a *App) stopInstalledService() error {
	installed, err := a.serviceInstalled()
	if err != nil || !installed || !a.daemonRunning() {
		return err
	}
	s, err := a.serviceSpec()
	if err != nil {
		return err
	}
	if s.Platform == "darwin" {
		// The monitor observes the disabled setting within 500 ms and exits 0.
		// launchd must see a successful exit so KeepAlive does not restart it.
		return nil
	}
	_, err = a.runServiceCommand("systemctl", "--user", "stop", systemdUnit)
	return err
}

func secureServiceDirectory(home, directory string) error {
	relative, err := filepath.Rel(home, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return errors.New("service directory is outside the user home")
	}
	current := home
	for _, component := range strings.Split(relative, string(os.PathSeparator)) {
		current = filepath.Join(current, component)
		if err := os.Mkdir(current, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to use non-directory or symlink service path %s", current)
		}
	}
	return nil
}

func (a *App) prepareServiceFile(s serviceSpec) error {
	if err := secureServiceDirectory(filepath.Dir(a.DefaultHome), filepath.Dir(s.Path)); err != nil {
		return err
	}
	if s.Platform == "darwin" {
		if err := os.MkdirAll(filepath.Dir(s.LogPath), 0700); err != nil {
			return err
		}
		info, err := os.Lstat(s.LogPath)
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to use non-regular service log %s", s.LogPath)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		logFile, err := os.OpenFile(s.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if err = logFile.Close(); err != nil {
			return err
		}
		return os.Chmod(s.LogPath, 0600)
	}
	return nil
}

func (a *App) installService() error {
	s, err := a.serviceSpec()
	if err != nil {
		return err
	}
	info, err := os.Stat(s.Binary)
	if err != nil || !isRunnable(info) {
		return errors.New("XSwap executable is unavailable; reinstall XSwap before installing its service")
	}
	previous, existed, err := s.readOwned()
	if err != nil {
		return err
	}
	definition, err := s.definition()
	if err != nil {
		return err
	}
	settings, err := a.settings()
	if err != nil {
		return err
	}
	if settings.Auto.Enabled {
		if err := a.configureAuto(false, 0, 0); err != nil {
			_ = a.configureAuto(true, 0, 0)
			return err
		}
		deadline := time.Now().Add(5 * time.Second)
		for a.daemonRunning() && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if a.daemonRunning() {
			_ = a.configureAuto(true, 0, 0)
			return errors.New("monitor did not stop; service installation cancelled")
		}
	}
	installErr := a.installServiceFile(s, definition, previous, existed)
	if settings.Auto.Enabled {
		restoreErr := a.configureAuto(true, 0, 0)
		if installErr != nil {
			return errors.Join(installErr, restoreErr)
		}
		return restoreErr
	}
	return installErr
}

func (a *App) installServiceFile(s serviceSpec, definition, previous []byte, existed bool) error {
	if err := a.prepareServiceFile(s); err != nil {
		return err
	}
	if existed && bytes.Equal(previous, definition) && a.serviceManagerLoaded(s) {
		return nil
	}
	if existed {
		if err := a.unregisterService(s); err != nil {
			return err
		}
	}
	if err := atomicWrite(s.Path, definition); err != nil {
		if existed {
			return errors.Join(err, a.registerService(s))
		}
		return err
	}
	if err := a.registerService(s); err != nil {
		var rollbackErr error
		if existed {
			rollbackErr = atomicWrite(s.Path, previous)
			if rollbackErr == nil {
				rollbackErr = a.registerService(s)
			}
		} else {
			rollbackErr = os.Remove(s.Path)
		}
		return errors.Join(fmt.Errorf("register XSwap service: %w", err), rollbackErr)
	}
	return nil
}

func (a *App) uninstallService() error {
	s, err := a.serviceSpec()
	if err != nil {
		return err
	}
	_, installed, err := s.readOwned()
	if err != nil || !installed {
		return err
	}
	if err := a.unregisterService(s); err != nil {
		return err
	}
	if err := os.Remove(s.Path); err != nil {
		return err
	}
	if s.Platform == "linux" {
		_, err = a.runServiceCommand("systemctl", "--user", "daemon-reload")
	}
	return err
}

func (a *App) serviceCommand(args []string) error {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return errors.New("xswap service is supported on macOS and Linux only")
	}
	if len(args) == 0 {
		args = []string{"status"}
	}
	if len(args) != 1 {
		return errors.New("usage: xswap service install|status|logs|uninstall")
	}
	switch args[0] {
	case "install":
		if err := a.installService(); err != nil {
			return err
		}
		fmt.Println("XSwap user service installed. Enable rotation with: xswap auto on")
	case "status":
		s, err := a.serviceSpec()
		if err != nil {
			return err
		}
		_, installed, err := s.readOwned()
		if err != nil {
			return err
		}
		settings, err := a.settings()
		if err != nil {
			return err
		}
		fmt.Printf("Service installed: %t\n", installed)
		if installed {
			fmt.Printf("Supervisor registered: %t\n", a.serviceManagerLoaded(s))
			fmt.Println("Service file:", s.Path)
		}
		fmt.Printf("Auto-switch enabled: %t\nMonitor running: %t\n", settings.Auto.Enabled, a.daemonRunning())
	case "logs":
		return a.serviceLogs(os.Stdout)
	case "uninstall":
		if err := a.uninstallService(); err != nil {
			return err
		}
		fmt.Println("XSwap user service removed. Auto-switch settings were preserved.")
	default:
		return errors.New("usage: xswap service install|status|logs|uninstall")
	}
	return nil
}

func (a *App) serviceLogs(out io.Writer) error {
	s, err := a.serviceSpec()
	if err != nil {
		return err
	}
	_, installed, err := s.readOwned()
	if err != nil {
		return err
	}
	if !installed {
		return errors.New("XSwap user service is not installed")
	}
	if s.Platform == "linux" {
		logs, err := a.runServiceCommand("journalctl", "--user", "-u", systemdUnit, "--no-pager", "-n", "100")
		if err != nil {
			return err
		}
		_, err = io.WriteString(out, logs)
		return err
	}
	file, err := os.Open(s.LogPath)
	if errors.Is(err, os.ErrNotExist) {
		_, err = io.WriteString(out, "No service logs yet.\n")
		return err
	}
	if err != nil {
		return err
	}
	defer file.Close()
	lines := make([]string, 0, 100)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		if len(lines) == 100 {
			copy(lines, lines[1:])
			lines = lines[:99]
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(lines) == 0 {
		_, err = io.WriteString(out, "No service logs yet.\n")
		return err
	}
	_, err = io.WriteString(out, strings.Join(lines, "\n")+"\n")
	return err
}
