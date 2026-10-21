//go:build !cli

/*
 * SPDX-License-Identifier: GPL-3.0
 * Distra Installer, a fork of the Equicord and Vencord installers
 * Copyright (c) 2026 Vendicated, Equicord and Distra contributors
 */

package main

import (
	"errors"
	"os/exec"
	path "path/filepath"
)

// restartDiscord closes the given install if it's running and starts it again through Discord's updater.
func restartDiscord(di *DiscordInstall) error {
	PreparePatch(di)
	update := path.Join(di.path, "Update.exe")
	if !ExistsFile(update) {
		return errors.New("Couldn't find " + update + ".")
	}
	return exec.Command(update, "--processStart", windowsNames[di.branch]+".exe").Start()
}
