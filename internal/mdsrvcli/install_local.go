package mdsrvcli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type installLocalReport struct {
	OK          bool              `json:"ok"`
	Binary      string            `json:"binary"`
	Home        string            `json:"home"`
	Completions map[string]string `json:"completions"`
	Overwritten bool              `json:"overwritten,omitempty"`
	WasSymlink  bool              `json:"was_symlink,omitempty"`
}

func (a app) runInstallLocal(ctx context.Context, flags *installFlags) (installLocalReport, error) {
	home, err := detectMDSrvInstallHome(flags.home)
	if err != nil {
		return installLocalReport{}, err
	}
	binDir, err := mdsrvInstallBinDir(flags.binDir)
	if err != nil {
		return installLocalReport{}, err
	}
	completionDir, err := mdsrvCompletionDir(flags.completionDir)
	if err != nil {
		return installLocalReport{}, err
	}
	if strings.TrimSpace(flags.name) == "" || strings.ContainsRune(flags.name, os.PathSeparator) {
		return installLocalReport{}, fmt.Errorf("invalid executable name %q", flags.name)
	}
	target := filepath.Join(binDir, flags.name)
	overwritten := false
	wasSymlink := false
	if info, err := os.Lstat(target); err == nil {
		if !flags.force {
			return installLocalReport{}, fmt.Errorf("%s already exists; pass --force to overwrite", target)
		}
		overwritten = true
		wasSymlink = info.Mode()&os.ModeSymlink != 0
	} else if err != nil && !os.IsNotExist(err) {
		return installLocalReport{}, err
	}
	tmpDir, err := os.MkdirTemp("", "hlmdsrv-install-*")
	if err != nil {
		return installLocalReport{}, err
	}
	defer os.RemoveAll(tmpDir)
	tmpBinary := filepath.Join(tmpDir, flags.name)
	build := exec.CommandContext(ctx, "go", "build", "-o", tmpBinary, "./cmd/hlmdsrv")
	build.Dir = home
	output, err := build.CombinedOutput()
	if err != nil {
		return installLocalReport{}, fmt.Errorf("go build failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return installLocalReport{}, err
	}
	if err := installMDSrvBinary(tmpBinary, target, 0o755, flags.force); err != nil {
		return installLocalReport{}, err
	}
	completions, err := a.writeInstallCompletions(completionDir, flags.name, flags.force)
	if err != nil {
		return installLocalReport{}, err
	}
	return installLocalReport{
		OK:          true,
		Binary:      target,
		Home:        home,
		Completions: completions,
		Overwritten: overwritten,
		WasSymlink:  wasSymlink,
	}, nil
}

func detectMDSrvInstallHome(value string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return validateMDSrvInstallHome(value)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(cwd, "cmd", "hlmdsrv", "main.go")); err == nil {
				return validateMDSrvInstallHome(cwd)
			}
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return "", fmt.Errorf("could not find headlessmolstar checkout; pass --home")
		}
		cwd = parent
	}
}

func validateMDSrvInstallHome(value string) (string, error) {
	home, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	for _, path := range []string{
		filepath.Join(home, "go.mod"),
		filepath.Join(home, "cmd", "hlmdsrv", "main.go"),
	} {
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
	}
	return home, nil
}

func mdsrvInstallBinDir(value string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return filepath.Abs(value)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin"), nil
}

func mdsrvCompletionDir(value string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return filepath.Abs(value)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "hlmdsrv", "completions"), nil
}

func (a app) writeInstallCompletions(dir string, commandName string, force bool) (map[string]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	paths := map[string]string{
		"bash": filepath.Join(dir, commandName+".bash"),
		"zsh":  filepath.Join(dir, "_"+commandName),
		"fish": filepath.Join(dir, commandName+".fish"),
	}
	for shell, path := range paths {
		if err := ensureOutputPath(path, force); err != nil {
			return nil, err
		}
		file, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		root := a.rootCommand()
		root.Use = commandName
		switch shell {
		case "bash":
			err = root.GenBashCompletion(file)
		case "zsh":
			err = root.GenZshCompletion(file)
		case "fish":
			err = root.GenFishCompletion(file, true)
		}
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	return paths, nil
}

func installMDSrvBinary(src, dst string, mode os.FileMode, force bool) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		if !force {
			return fmt.Errorf("%s already exists; pass --force to overwrite", dst)
		}
		if err := os.Remove(dst); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	in, err := os.Open(src)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = in.Close()
		_ = tmp.Close()
		return err
	}
	if err := in.Close(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	cleanup = false
	return os.Chmod(dst, mode)
}
