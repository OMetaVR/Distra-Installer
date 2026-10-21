//go:build !cli

/*
 * SPDX-License-Identifier: GPL-3.0
 * Distra Installer, a fork of the Equicord and Vencord installers
 * Copyright (c) 2026 Vendicated, Equicord and Distra contributors
 */

package main

// Runs install, repair, uninstall and OpenAsar operations in the background and reports real step progress
// to the "working" screen.

import (
	"errors"
	path "path/filepath"
	"sync"
	"time"
)

type opKind int

const (
	opInstall opKind = iota
	opRepair
	opUninstall
	opOpenAsar
)

// Every step stays on screen at least this long, so fast steps are still readable.
const minStepTime = 450 * time.Millisecond

type operation struct {
	kind   opKind
	target *DiscordInstall
	name   string
	steps  []string

	mu              sync.Mutex
	step            int       // active step; len(steps) once all are done
	stepFraction    float32   // progress within the active step, 0-1
	stepStarted     time.Time // when the active step began
	stepDoneAt      []time.Time
	removedOpenAsar bool

	shownProgress float32 // animated progress bar value, only touched by the UI
}

var currentOp *operation

// githubDone is set once the release lookup started at launch has finished, successfully or not.
var githubDone bool

func installName(di *DiscordInstall) string {
	switch di.branch {
	case "ptb":
		return "Discord PTB"
	case "canary":
		return "Discord Canary"
	case "development":
		return "Discord Development"
	case "stable", "":
		return "Discord Stable"
	}
	return "Discord"
}

func startOperation(kind opKind, di *DiscordInstall) {
	op := &operation{kind: kind, target: di, name: installName(di)}
	switch kind {
	case opInstall, opRepair:
		patching := "Patching " + op.name
		op.steps = []string{"Fetching latest release", "Downloading Distra", patching, "Verifying the patch"}
	case opUninstall:
		op.steps = []string{"Locating " + op.name, "Restoring original Discord files", "Verifying the restore"}
	case opOpenAsar:
		op.removedOpenAsar = di.IsOpenAsar()
		op.steps = []string{Ternary(op.removedOpenAsar, "Restoring Discord's app.asar in ", "Installing OpenAsar into ") + op.name, "Verifying the change"}
	}
	op.stepDoneAt = make([]time.Time, len(op.steps))
	op.stepStarted = time.Now()

	currentOp = op
	setScreen(screenWorking)
	go op.run()
}

func (op *operation) begin(step int) {
	op.mu.Lock()
	op.step, op.stepFraction, op.stepStarted = step, 0, time.Now()
	op.mu.Unlock()
}

func (op *operation) setFraction(f float32) {
	op.mu.Lock()
	op.stepFraction = clamp01(f)
	op.mu.Unlock()
}

func (op *operation) finish(step int) {
	op.mu.Lock()
	started := op.stepStarted
	op.mu.Unlock()
	if wait := minStepTime - time.Since(started); wait > 0 {
		time.Sleep(wait)
	}
	op.mu.Lock()
	op.stepDoneAt[step] = time.Now()
	op.step, op.stepFraction = step+1, 0
	op.mu.Unlock()
}

// progress is the overall completion, 0-1, from finished steps plus the active step's own progress.
func (op *operation) progress() float32 {
	op.mu.Lock()
	defer op.mu.Unlock()
	return (float32(op.step) + op.stepFraction) / float32(len(op.steps))
}

func (op *operation) snapshot() (step int, doneAt []time.Time) {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.step, append([]time.Time(nil), op.stepDoneAt...)
}

func (op *operation) run() {
	var failed bool
	switch op.kind {
	case opInstall, opRepair:
		failed = op.runPatch()
	case opUninstall:
		failed = op.runUnpatch()
	case opOpenAsar:
		failed = op.runOpenAsar()
	}

	if failed {
		setScreen(screenIdle)
		return
	}
	time.Sleep(250 * time.Millisecond)
	setScreen(screenDone)
}

func (op *operation) runPatch() (failed bool) {
	di := op.target

	op.begin(0)
	for !IsDevInstall && !githubDone {
		time.Sleep(50 * time.Millisecond)
	}
	if !IsDevInstall && GithubError != nil {
		ShowModal("Couldn't reach GitHub", "Distra's latest release couldn't be fetched, so nothing was changed. If this keeps happening, visit "+SupportUrl+" for help.", GithubError.Error())
		return true
	}
	op.finish(0)

	op.begin(1)
	if !IsDevInstall && (op.kind == opRepair || LatestHash != InstalledHash) {
		DownloadProgress = func(read, total int64) {
			if total > 0 {
				op.setFraction(float32(read) / float32(total))
			}
		}
		err := installLatestBuilds()
		DownloadProgress = nil
		if err != nil {
			ShowModal("Failed to download Distra", "Nothing was changed. If this keeps happening, visit "+SupportUrl+" for help.", err.Error())
			return true
		}
	}
	op.finish(1)

	op.begin(2)
	if CheckScuffedInstall() {
		return true
	}
	if err := di.patch(); err != nil {
		handleErr(di, err, "patch")
		return true
	}
	op.finish(2)

	op.begin(3)
	if ok, err := isEquicordLoaderAppAsar(path.Join(di.asarDir(), "app.asar")); err != nil || !ok {
		handleErr(di, Ternary(err != nil, err, errors.New("Discord's app.asar doesn't load Distra after patching.")), "verify the patch on")
		return true
	}
	op.finish(3)
	return false
}

func (op *operation) runUnpatch() (failed bool) {
	di := op.target

	op.begin(0)
	if !ExistsFile(di.path) {
		handleErr(di, errors.New(di.path+" no longer exists."), "find")
		return true
	}
	op.finish(0)

	op.begin(1)
	if err := di.unpatch(); err != nil {
		handleErr(di, err, "unpatch")
		return true
	}
	op.finish(1)

	op.begin(2)
	if ok, _ := isEquicordLoaderAppAsar(path.Join(di.asarDir(), "app.asar")); ok {
		handleErr(di, errors.New("Discord's app.asar still loads Distra."), "verify the restore on")
		return true
	}
	op.finish(2)
	return false
}

func (op *operation) runOpenAsar() (failed bool) {
	di := op.target

	op.begin(0)
	if op.removedOpenAsar {
		if err := di.UninstallOpenAsar(); err != nil {
			handleErr(di, err, "uninstall OpenAsar from")
			return true
		}
	} else if err := di.InstallOpenAsar(); err != nil {
		handleErr(di, err, "install OpenAsar on")
		return true
	}
	op.finish(0)

	op.begin(1)
	di.isOpenAsar = nil
	if di.IsOpenAsar() == op.removedOpenAsar {
		handleErr(di, errors.New("Discord's app.asar didn't change."), Ternary(op.removedOpenAsar, "uninstall OpenAsar from", "install OpenAsar on"))
		return true
	}
	op.finish(1)
	return false
}

// asarDir is the folder holding the app.asar the patcher replaces.
func (di *DiscordInstall) asarDir() string {
	if di.isSystemElectron {
		return di.path
	}
	return path.Join(di.appPath, "..")
}

func (op *operation) titles() (working, done, subtitle string) {
	restart := "Restart " + op.name + " to finish. You'll find the Distra section in User Settings."
	switch op.kind {
	case opRepair:
		return "Repairing Distra", "Distra was repaired", restart
	case opUninstall:
		return "Removing Distra", "Distra was removed", op.name + " has been restored to a clean, unpatched install. You can close this window."
	case opOpenAsar:
		if op.removedOpenAsar {
			return "Removing OpenAsar", "OpenAsar was removed", "Restart " + op.name + " to go back to Discord's original app.asar."
		}
		return "Installing OpenAsar", "OpenAsar is installed", "Restart " + op.name + " to start using OpenAsar."
	}
	return "Installing Distra", "Distra is installed", restart
}
