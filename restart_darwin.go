//go:build !cli

/*
 * SPDX-License-Identifier: GPL-3.0
 * Distra Installer, a fork of the Equicord and Vencord installers
 * Copyright (c) 2026 Vendicated, Equicord and Distra contributors
 */

package main

import (
	"os/exec"
	path "path/filepath"
	"strings"
	"time"
)

// restartDiscord quits the given install if it's running and opens it again.
func restartDiscord(di *DiscordInstall) error {
	name := strings.TrimSuffix(path.Base(di.path), ".app")
	if exec.Command("osascript", "-e", `quit app "`+name+`"`).Run() == nil {
		time.Sleep(1500 * time.Millisecond)
	}
	return exec.Command("open", "-a", di.path).Start()
}
