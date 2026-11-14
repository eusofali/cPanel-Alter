// Package sys is the only place the panel touches the host system.
// In dev mode every path is redirected under Root and commands are logged
// instead of executed, so the panel can run safely on a workstation.
package sys

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Sys struct {
	Dev  bool
	Root string // prefix for all paths in dev mode
	Log  *slog.Logger

	once     sync.Once
	wwwUID   int
	wwwGID   int
	wwwFound bool
}

// Path maps an absolute system path to where it really lives.
func (s *Sys) Path(p string) string {
	if s.Dev {
		return filepath.Join(s.Root, p)
	}
	return p
}

// Run executes a command with optional stdin and returns combined output.
func (s *Sys) Run(stdin, name string, args ...string) (string, error) {
	return s.RunTimeout(3*time.Minute, stdin, name, args...)
}

// RunTimeout is Run with a caller-chosen limit, for slow jobs like apt.
func (s *Sys) RunTimeout(limit time.Duration, stdin, name string, args ...string) (string, error) {
	if s.Dev {
		s.Log.Info("dev: command skipped", "cmd", name+" "+strings.Join(args, " "))
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C")
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		if len(text) > 2000 {
			text = text[len(text)-2000:]
		}
		return text, fmt.Errorf("%s failed: %s", name, text)
	}
	return text, nil
}

// WriteFile writes atomically, creating parent directories.
func (s *Sys) WriteFile(path string, data []byte, perm os.FileMode) error {
	p := s.Path(path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".hostdeck-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (s *Sys) ReadFile(path string) ([]byte, error) { return os.ReadFile(s.Path(path)) }

func (s *Sys) Remove(path string) error {
	err := os.Remove(s.Path(path))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Sys) RemoveAll(path string) error { return os.RemoveAll(s.Path(path)) }

func (s *Sys) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(s.Path(path), perm)
}

func (s *Sys) Exists(path string) bool {
	_, err := os.Stat(s.Path(path))
	return err == nil
}

func (s *Sys) Glob(pattern string) []string {
	m, _ := filepath.Glob(s.Path(pattern))
	return m
}

// WebUser returns the uid/gid of www-data (33 on Debian/Ubuntu as fallback).
func (s *Sys) WebUser() (int, int) {
	s.once.Do(func() {
		s.wwwUID, s.wwwGID = 33, 33
		if u, err := user.Lookup("www-data"); err == nil {
			uid, e1 := strconv.Atoi(u.Uid)
			gid, e2 := strconv.Atoi(u.Gid)
			if e1 == nil && e2 == nil {
				s.wwwUID, s.wwwGID, s.wwwFound = uid, gid, true
			}
		}
	})
	return s.wwwUID, s.wwwGID
}

// ChownWeb gives a real (already resolved) path to the web user. No-op in dev.
func (s *Sys) ChownWeb(realPath string) {
	if s.Dev {
		return
	}
	uid, gid := s.WebUser()
	if s.wwwFound {
		_ = os.Lchown(realPath, uid, gid)
	}
}
