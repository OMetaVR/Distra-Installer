//go:build !cli

/*
 * SPDX-License-Identifier: GPL-3.0
 * Distra Installer, a fork of the Equicord and Vencord installers
 * Copyright (c) 2026 Vendicated, Equicord and Distra contributors
 */

package main

import (
	"errors"
	"os"
	"os/exec"
	path "path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// restartDiscord closes the given install if it's running and starts it again.
func restartDiscord(di *DiscordInstall) error {
	if os.Geteuid() == 0 {
		return errors.New("The installer is running as root, so it won't start Discord for you.")
	}

	if di.isFlatpak {
		id := ""
		for _, e := range strings.Split(di.path, "/") {
			if strings.HasPrefix(e, "com.discordapp") {
				id = e
			}
		}
		if id == "" {
			return errors.New("Couldn't tell which Flatpak " + di.path + " belongs to.")
		}
		_ = exec.Command("flatpak", "kill", id).Run()
		return startDetached("flatpak", "run", id)
	}

	root := di.path
	if resolved, err := path.EvalSymlinks(root); err == nil {
		root = resolved
	}
	stopProcessesUnder(root)

	exe := findDiscordExecutable(di)
	if exe == "" {
		return errors.New("Couldn't find Discord's executable in " + di.path + ".")
	}
	return startDetached(exe)
}

// stopProcessesUnder ends every process whose executable or command line lives under root.
func stopProcessesUnder(root string) {
	entries, _ := os.ReadDir("/proc")
	var procs []*os.Process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		exe, _ := os.Readlink(path.Join("/proc", e.Name(), "exe"))
		cmdline, _ := os.ReadFile(path.Join("/proc", e.Name(), "cmdline"))
		if strings.HasPrefix(exe, root+"/") || strings.Contains(string(cmdline), root+"/") {
			if p, err := os.FindProcess(pid); err == nil {
				procs = append(procs, p)
			}
		}
	}
	for _, p := range procs {
		_ = p.Signal(syscall.SIGTERM)
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		alive := false
		for _, p := range procs {
			if p.Signal(syscall.Signal(0)) == nil {
				alive = true
			}
		}
		if !alive {
			return
		}
	}
	for _, p := range procs {
		_ = p.Kill()
	}
}

func findDiscordExecutable(di *DiscordInstall) string {
	names := []string{"Discord", "DiscordCanary", "DiscordPTB", "DiscordDevelopment", "discord", "discord-canary", "discord-ptb"}
	for _, n := range names {
		p := path.Join(di.path, n)
		if s, err := os.Stat(p); err == nil && !s.IsDir() && s.Mode()&0o111 != 0 {
			return p
		}
	}
	system := map[string]string{"stable": "discord", "ptb": "discord-ptb", "canary": "discord-canary", "development": "discord-development"}
	if p, err := exec.LookPath(system[di.branch]); err == nil {
		return p
	}
	return ""
}

func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
