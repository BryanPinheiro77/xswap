package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const maxConfigExport = 1 << 20

// This allowlist intentionally contains no Codex files, identity observations,
// active selection, paths, or arbitrary account metadata.
type configExport struct {
	Format   string          `json:"format"`
	Version  int             `json:"version"`
	Accounts []exportAccount `json:"accounts"`
	Auto     *AutoConfig     `json:"auto"`
}

type exportAccount struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Disabled bool   `json:"disabled"`
}

type importOptions struct {
	SkipExisting bool
	ApplyAuto    bool
	DryRun       bool
}

type importResult struct {
	Created []string
	Skipped []string
}

func (document configExport) validate() error {
	if document.Format != "xswap-config" || document.Version != 1 {
		return errors.New("unsupported configuration export format or version")
	}
	if document.Accounts == nil || len(document.Accounts) > 1024 || document.Auto == nil {
		return errors.New("configuration export requires accounts and auto settings (maximum 1024 accounts)")
	}
	if err := document.Auto.validate(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, account := range document.Accounts {
		key := strings.ToLower(account.Name)
		if validate(account.Name) != nil || (key == "default" && account.Name != "default") {
			return errors.New("configuration export contains an invalid account name")
		}
		if seen[key] {
			return errors.New("configuration export contains duplicate account names (case-insensitive)")
		}
		seen[key] = true
		if len([]rune(account.Label)) > 64 || account.Label != strings.TrimSpace(account.Label) || strings.ContainsFunc(account.Label, unicode.IsControl) {
			return errors.New("configuration export contains an invalid display label")
		}
	}
	return nil
}

func readConfigExport(input io.Reader) (configExport, error) {
	var document configExport
	data, err := io.ReadAll(io.LimitReader(input, maxConfigExport+1))
	if err != nil {
		return document, errors.New("cannot read configuration export")
	}
	if len(data) > maxConfigExport {
		return document, errors.New("configuration export exceeds the 1 MiB limit")
	}
	// encoding/json otherwise accepts duplicate keys and case aliases. Reject
	// both, null required fields, and deeply nested input before decoding.
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := checkExportJSON(decoder, 0, false); err != nil {
		return document, errors.New("invalid configuration export JSON")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return document, errors.New("configuration export must contain exactly one JSON object")
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		// Parser errors can contain arbitrary input. Never echo them to a terminal.
		return document, errors.New("invalid configuration export fields")
	}
	return document, document.validate()
}

func checkExportJSON(decoder *json.Decoder, depth int, nullable bool) error {
	if depth > 16 {
		return errors.New("export nesting limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil && !nullable {
		return errors.New("null export field")
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		allowNull := false
		if delimiter == '{' {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[strings.ToLower(key)] {
				return errors.New("duplicate export field")
			}
			seen[strings.ToLower(key)] = true
			allowNull = key == "threshold5h" || key == "threshold7d"
		}
		if err := checkExportJSON(decoder, depth+1, allowNull); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

// Validate manager-owned parent directories before taking the lock and again
// under it. Profile symlinks and unrelated leftover directories are never reused.
func (a *App) checkTransferStorage() error {
	for _, path := range []string{a.Root, filepath.Join(a.Root, "profiles"), filepath.Join(a.Root, "health")} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("configuration transfer requires real manager directories, not symlinks")
		}
	}
	info, err := os.Lstat(filepath.Join(a.Root, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("settings.json must be a regular file")
	}
	return nil
}

func (a *App) exportConfig() (configExport, error) {
	document := configExport{Format: "xswap-config", Version: 1, Accounts: []exportAccount{}}
	if err := a.checkTransferStorage(); err != nil {
		return document, err
	}
	err := a.withState(func() error {
		if err := a.checkTransferStorage(); err != nil {
			return err
		}
		settings, err := a.settings()
		if err != nil {
			return errors.New("cannot export invalid manager settings")
		}
		entries, err := os.ReadDir(filepath.Join(a.Root, "profiles"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		names := []string{"default"}
		for _, entry := range entries {
			if validate(entry.Name()) == nil && entry.Name() != "default" && registeredProfile(filepath.Join(a.Root, "profiles", entry.Name())) {
				names = append(names, entry.Name())
			}
		}
		for _, name := range names {
			document.Accounts = append(document.Accounts, exportAccount{Name: name, Label: settings.DisplayNames[name], Disabled: disabled(settings, name)})
		}
		document.Auto = &settings.Auto
		return document.validate()
	})
	return document, err
}

func (a *App) planConfigImport(document configExport, options importOptions) (Settings, importResult, error) {
	var result importResult
	if err := a.checkTransferStorage(); err != nil {
		return Settings{}, result, err
	}
	settings, err := a.settings()
	if err != nil || len(settings.UnknownKeys) > 0 {
		return settings, result, errors.New("resolve invalid or unknown local settings before importing")
	}
	entries, err := os.ReadDir(filepath.Join(a.Root, "profiles"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return settings, result, err
	}
	existing := map[string]string{}
	for _, entry := range entries {
		existing[strings.ToLower(entry.Name())] = entry.Name()
	}
	for _, account := range document.Accounts {
		if account.Name == "default" {
			result.Skipped = append(result.Skipped, account.Name)
			continue
		}
		if name, exists := existing[strings.ToLower(account.Name)]; exists {
			if options.SkipExisting && registeredProfile(filepath.Join(a.Root, "profiles", name)) {
				result.Skipped = append(result.Skipped, account.Name)
				continue
			}
			return settings, result, fmt.Errorf("account path %q already exists; --skip-existing only skips registered profiles", account.Name)
		}
		// The settings file may still have entries for a previously removed name.
		next := settings.Disabled[:0]
		for _, name := range settings.Disabled {
			if name != account.Name {
				next = append(next, name)
			}
		}
		settings.Disabled = next
		if account.Disabled {
			settings.Disabled = append(settings.Disabled, account.Name)
		}
		delete(settings.DisplayNames, account.Name)
		if account.Label != "" {
			settings.DisplayNames[account.Name] = account.Label
		}
		result.Created = append(result.Created, account.Name)
	}
	if options.ApplyAuto {
		enabled := settings.Auto.Enabled
		settings.Auto = *document.Auto
		settings.Auto.Enabled = enabled
	}
	sort.Strings(settings.Disabled)
	return settings, result, nil
}

func (a *App) importConfig(document configExport, options importOptions) (importResult, error) {
	var result importResult
	if err := document.validate(); err != nil {
		return result, err
	}
	if options.DryRun {
		_, result, err := a.planConfigImport(document, options)
		return result, err
	}
	if err := a.checkTransferStorage(); err != nil {
		return result, err
	}
	err := a.withState(func() (importErr error) {
		settings, plan, err := a.planConfigImport(document, options)
		if err != nil {
			return err
		}
		result = plan
		if len(plan.Created) == 0 && !options.ApplyAuto {
			return nil
		}
		stage, err := os.MkdirTemp(a.Root, ".import-")
		if err != nil {
			return err
		}
		cleanupStage := true
		defer func() {
			if cleanupStage {
				_ = os.RemoveAll(stage)
			}
		}()
		for _, name := range plan.Created {
			if err := atomicWrite(filepath.Join(stage, "profiles", name, profileMarker), []byte("1\n")); err != nil {
				return err
			}
		}
		if len(plan.Created) > 0 {
			if err := os.MkdirAll(filepath.Join(a.Root, "profiles"), 0700); err != nil {
				return err
			}
		}
		var created, oldHealth []string
		committed := false
		defer func() {
			if !committed {
				for _, name := range created {
					// Remove only the marker and the empty directory we published.
					// Never delete runtime files created concurrently by another process.
					for _, path := range []string{filepath.Join(a.Root, "profiles", name, profileMarker), filepath.Join(a.Root, "profiles", name)} {
						if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
							importErr = errors.Join(importErr, fmt.Errorf("profile rollback incomplete for %q; inspect its directory before retrying", name))
						}
					}
				}
				for _, name := range oldHealth {
					if err := os.Rename(filepath.Join(stage, "health-"+name), a.healthPath(name)); err != nil {
						cleanupStage = false
						importErr = errors.Join(importErr, fmt.Errorf("health rollback failed; recovery data retained at %s", stage))
					}
				}
			}
		}()
		for _, name := range plan.Created {
			if info, err := os.Lstat(a.healthPath(name)); err == nil {
				if !info.Mode().IsRegular() {
					return errors.New("existing account health must be a regular file")
				}
				if err := os.Rename(a.healthPath(name), filepath.Join(stage, "health-"+name)); err != nil {
					return err
				}
				oldHealth = append(oldHealth, name)
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			path := filepath.Join(a.Root, "profiles", name)
			if err := os.Mkdir(path, 0700); err != nil {
				return err
			}
			created = append(created, name)
			if err := os.Rename(filepath.Join(stage, "profiles", name, profileMarker), filepath.Join(path, profileMarker)); err != nil {
				return err
			}
		}
		if err := writeJSON(filepath.Join(a.Root, "settings.json"), settings); err != nil {
			return err
		}
		committed = true
		return nil
	})
	if err != nil {
		return importResult{}, err
	}
	return result, nil
}

func (a *App) transferCommand(action string, args []string, out io.Writer) error {
	o, err := parse(args)
	if err != nil {
		return err
	}
	if o.Flags["help"] {
		_, err := fmt.Fprintln(out, "usage: xswap export FILE | import FILE [--skip-existing] [--apply-auto] [--dry-run]")
		return err
	}
	if len(o.Names) != 1 || len(o.Values) != 0 {
		return errors.New("usage: xswap export FILE | import FILE [--skip-existing] [--apply-auto] [--dry-run]")
	}
	for flag := range o.Flags {
		if action == "export" || (flag != "skip-existing" && flag != "apply-auto" && flag != "dry-run") {
			return errors.New("unsupported configuration transfer option")
		}
	}
	path := o.Names[0]
	if err := a.checkTransferFilePath(path); err != nil {
		return err
	}
	if action == "export" {
		document, err := a.exportConfig()
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("cannot create export file; choose a new file in an existing directory")
		}
		_, writeErr := file.Write(append(data, '\n'))
		closeErr := file.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			_ = os.Remove(path)
			return err
		}
		_, err = fmt.Fprintln(out, "Configuration exported. Credentials and sessions are not included.")
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("import file must be a regular file, not a symlink")
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("cannot open configuration export")
	}
	document, err := readConfigExport(file)
	file.Close()
	if err != nil {
		return err
	}
	options := importOptions{SkipExisting: o.Flags["skip-existing"], ApplyAuto: o.Flags["apply-auto"], DryRun: o.Flags["dry-run"]}
	result, err := a.importConfig(document, options)
	if err != nil {
		return err
	}
	verb := "Imported"
	if options.DryRun {
		verb = "Would import"
	}
	_, err = fmt.Fprintf(out, "%s %d account(s); skipped %d. Active selection and auto-switch enabled state are preserved.\nSign in to imported accounts with: xswap login NAME\n", verb, len(result.Created), len(result.Skipped))
	return err
}

// Keep user-selected transfer files out of live account storage, even through a
// symlinked parent. Exclusive output creation also protects unrelated files.
func (a *App) checkTransferFilePath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return errors.New("transfer file requires an existing parent directory")
	}
	absolute = filepath.Join(parent, filepath.Base(absolute))
	for _, root := range []string{a.Root, a.DefaultHome} {
		root, err = filepath.Abs(root)
		if err != nil {
			return err
		}
		if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
			root = resolved
		}
		relative, relErr := filepath.Rel(root, absolute)
		if relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("keep transfer files outside the manager and original Codex directories")
		}
	}
	return nil
}
