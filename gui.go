//go:build !cli

/*
 * SPDX-License-Identifier: GPL-3.0
 * Vencord Installer, a cross platform gui/cli app for installing Vencord
 * Copyright (c) 2023 Vendicated and Vencord contributors
 */

package main

import (
	"bytes"
	_ "embed"
	"errors"
	"image"
	"image/color"
	"math"
	"os"
	path "path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	g "github.com/AllenDang/giu"
	"github.com/AllenDang/imgui-go"

	// png decoder for icon
	_ "image/png"
)

var (
	discords        []any
	radioIdx        int
	customChoiceIdx int

	customDir              string
	customFocused          bool
	autoCompleteDir        string
	autoCompleteFile       string
	autoCompleteCandidates []string
	autoCompleteIdx        int
	lastAutoComplete       string
	didAutoComplete        bool

	modalId      = 0
	modalTitle   = "Oh No :("
	modalMessage = "You should never see this"
	modalExtra   = ""

	acceptedOpenAsar   bool
	showedUpdatePrompt bool

	win *g.MasterWindow
)

//go:embed winres/icon.png
var iconBytes []byte

func init() {
	LogLevel = LevelDebug
}

func main() {
	InitGithubDownloader()
	discords = FindDiscords()
	customChoiceIdx = len(discords)

	go func() {
		<-GithubDoneChan
		githubDone = true
		g.Update()
	}()

	go func() {
		<-SelfUpdateCheckDoneChan
		g.Update()
	}()

	if runtime.GOOS == "linux" {
		os.Setenv("GDK_SCALE", "1")
		os.Setenv("GDK_DPI_SCALE", "1")
	}

	loadFonts()
	win = g.NewMasterWindow("Distra Installer", 820, 660, 0)
	win.SetBgColor(colBackground)

	icon, _, err := image.Decode(bytes.NewReader(iconBytes))
	if err != nil {
		Log.Warn("Failed to load application icon", err)
	} else {
		win.SetIcon([]image.Image{icon})
	}

	setScreen(previewScreen())
	win.Run(loop)
}

// --- Screens ---

type screenID int

const (
	screenIdle screenID = iota
	screenWorking
	screenDone
)

var (
	screenMu    sync.Mutex
	screen      = screenIdle
	screenSince = time.Now()
)

func setScreen(s screenID) {
	screenMu.Lock()
	screen, screenSince = s, time.Now()
	screenMu.Unlock()
	if s == screenIdle {
		// Patching changes what each install reports, so look again.
		selected := radioIdx
		discords = FindDiscords()
		customChoiceIdx = len(discords)
		radioIdx = min(selected, customChoiceIdx)
	}
	keepAnimating(1500 * time.Millisecond)
	g.Update()
}

func currentScreen() (screenID, time.Time) {
	screenMu.Lock()
	defer screenMu.Unlock()
	return screen, screenSince
}

// previewScreen lets DISTRA_INSTALLER_PREVIEW=working|done open straight on those screens with sample progress,
// for checking the design without installing anything.
func previewScreen() screenID {
	kind := os.Getenv("DISTRA_INSTALLER_PREVIEW")
	if kind != "working" && kind != "done" {
		return screenIdle
	}
	op := &operation{kind: opInstall, target: &DiscordInstall{branch: "stable"}, name: "Discord Stable"}
	op.steps = []string{"Fetching latest release", "Downloading Distra", "Patching Discord Stable", "Verifying the patch"}
	op.stepDoneAt = []time.Time{time.Now(), time.Now(), {}, {}}
	op.step, op.stepFraction, op.shownProgress = 2, .32, .58
	currentOp = op
	return Ternary(kind == "working", screenWorking, screenDone)
}

// --- Popups. Operations run in the background, so popups are queued and opened on the next frame. ---

var (
	popupMu       sync.Mutex
	pendingPopups []string
)

func openPopup(id string) {
	popupMu.Lock()
	pendingPopups = append(pendingPopups, id)
	popupMu.Unlock()
	g.Update()
}

func flushPopups() {
	popupMu.Lock()
	ids := pendingPopups
	pendingPopups = nil
	popupMu.Unlock()
	for _, id := range ids {
		g.OpenPopup(id)
	}
}

func ShowModal(title, desc, extra string) {
	modalTitle = title
	modalMessage = desc
	modalExtra = extra
	modalId++
	openPopup("#modal" + strconv.Itoa(modalId))
}

func HandleScuffedInstall() {
	openPopup("#scuffed-install")
}

func HandleInsufficientPermissions() {
	openPopup("#insufficient-permissions")
}

// --- Actions ---

func getChosenInstall() *DiscordInstall {
	if radioIdx != customChoiceIdx {
		return discords[radioIdx].(*DiscordInstall)
	}

	if discord := ParseDiscord(customDir, ""); discord != nil {
		return discord
	}

	if discord := ParseDiscordNew(customDir, "", strings.Contains(customDir, "com.discordapp")); discord != nil {
		return discord
	}

	openPopup("#invalid-custom-location")
	return nil
}

func InstallLatestBuilds() (err error) {
	if IsDevInstall {
		return
	}

	err = installLatestBuilds()
	if err != nil {
		ShowModal("Failed to install the latest Distra builds from GitHub", "If this issue persists, visit "+SupportUrl+" for help.", err.Error())
	}
	return
}

func run(kind opKind) {
	if di := getChosenInstall(); di != nil {
		startOperation(kind, di)
	}
}

func handleOpenAsar() {
	di := getChosenInstall()
	if di == nil {
		return
	}
	if acceptedOpenAsar || di.IsOpenAsar() {
		startOperation(opOpenAsar, di)
		return
	}
	openPopup("#openasar-confirm")
}

func handleErr(di *DiscordInstall, err error, action string) {
	if errors.Is(err, ErrAlreadyReported) {
		return
	}
	if errors.Is(err, os.ErrPermission) {
		switch runtime.GOOS {
		case "windows":
			err = errors.New("Permission denied. Make sure your Discord is fully closed (from the tray)!")
		case "darwin":
			HandleInsufficientPermissions()
			return
		case "linux":
			command := "sudo chown -R \"$USER:$USER\" " + di.path
			err = errors.New("Permission denied. Try to run the installer with sudo privileges.\n\nIf that also doesn't work, try running the following command in your terminal:\n" + command)
		default:
			err = errors.New("Permission denied. Maybe try running me as Administrator/Root?")
		}
	}

	ShowModal("Failed to "+action+" this install", "If this issue persists, visit "+SupportUrl+" for help.", err.Error())
}

// Patch and Unpatch are kept for code shared with the CLI. The GUI goes through startOperation instead.
func (di *DiscordInstall) Patch() {
	if CheckScuffedInstall() {
		return
	}
	if err := di.patch(); err != nil {
		handleErr(di, err, "patch")
	}
}

func (di *DiscordInstall) Unpatch() {
	if err := di.unpatch(); err != nil {
		handleErr(di, err, "unpatch")
	}
}

// --- Custom location autocomplete (Tab completes the next matching entry) ---

func onCustomInputChanged() {
	p := customDir
	if len(p) != 0 {
		radioIdx = customChoiceIdx
	}

	dir := path.Dir(p)

	isNewDir := strings.HasSuffix(p, "/")
	wentUpADir := !isNewDir && dir != autoCompleteDir

	if isNewDir || wentUpADir {
		autoCompleteDir = dir
		autoCompleteIdx = 0
		lastAutoComplete = ""
		autoCompleteFile = ""
		autoCompleteCandidates = nil

		files, err := os.ReadDir(dir)
		if err == nil {
			for _, file := range files {
				autoCompleteCandidates = append(autoCompleteCandidates, file.Name())
			}
		}
	} else if !didAutoComplete {
		autoCompleteFile = path.Base(p)
		lastAutoComplete = ""
	}

	if wentUpADir {
		autoCompleteFile = path.Base(p)
	}

	didAutoComplete = false
}

func makeAutoComplete() []string {
	input := strings.ToLower(autoCompleteFile)

	var candidates []string
	for _, e := range autoCompleteCandidates {
		if autoCompleteFile == "" || strings.HasPrefix(strings.ToLower(e), input) {
			candidates = append(candidates, e)
		}
	}
	return candidates
}

func autoCompleteCallback(candidates []string) imgui.InputTextCallback {
	return func(data imgui.InputTextCallbackData) int32 {
		if len(candidates) == 0 {
			return 0
		}
		if autoCompleteIdx >= len(candidates) {
			autoCompleteIdx = 0
		}

		didAutoComplete = true

		start := len(customDir)
		if lastAutoComplete != "" {
			start -= len(lastAutoComplete)
			data.DeleteBytes(start, len(lastAutoComplete))
		} else if autoCompleteFile != "" {
			start -= len(autoCompleteFile)
			data.DeleteBytes(start, len(autoCompleteFile))
		}

		lastAutoComplete = candidates[autoCompleteIdx]
		data.InsertBytes(start, []byte(lastAutoComplete))
		autoCompleteIdx++

		return 0
	}
}

// --- Layout ---

const (
	padX      = 32
	padTop    = 26
	padBottom = 28
	gap       = 18
	footerH   = 38 + 16 + 1 // buttons, padding above them, divider
)

// advance moves the layout cursor below a block drawn at x, y, after any widgets inside it moved it.
func advance(x, y, w, h float32) {
	imgui.SetCursorScreenPos(v2(x, y))
	imgui.Dummy(v2(w, h))
}

func loop() {
	g.PushWindowPadding(0, 0)
	g.PushItemSpacing(0, 0)
	g.PushColorWindowBg(colBackground)
	defer g.PopStyleV(2)
	defer g.PopStyleColor()

	g.SingleWindow().
		RegisterKeyboardShortcuts(
			g.WindowShortcut{Key: g.KeyUp, Callback: func() {
				if radioIdx > 0 {
					radioIdx--
				}
			}},
			g.WindowShortcut{Key: g.KeyDown, Callback: func() {
				if radioIdx < customChoiceIdx {
					radioIdx++
				}
			}},
		).
		Layout(
			g.Custom(func() {
				s, since := currentScreen()
				switch s {
				case screenIdle:
					renderIdle(since)
				case screenWorking:
					keepAnimating(100 * time.Millisecond)
					renderWorking(since)
				case screenDone:
					renderDone(since)
				}
				drawAlpha = 1
			}),
			modals(),
			g.Custom(flushPopups),
		)
}

// --- Idle screen ---

func renderIdle(since time.Time) {
	if CanUpdateSelf() && !showedUpdatePrompt {
		showedUpdatePrompt = true
		openPopup("#update-prompt")
	}

	winW, winH := g.GetAvailableRegion()
	origin := imgui.CursorScreenPos()
	x0, y0 := origin.X+padX, origin.Y+padTop
	w := winW - 2*padX
	bodyH := winH - padTop - padBottom - footerH - gap

	// Everything above the footer scrolls if the window is too short for it.
	imgui.SetCursorScreenPos(v2(x0, y0))
	g.PushStyleColor(g.StyleColorChildBg, withAlpha(colBackground, 0))
	g.PushStyleColor(g.StyleColorScrollbarBg, colBackground)
	g.PushStyleColor(g.StyleColorScrollbarGrab, colDeep)
	imgui.PushStyleVarFloat(imgui.StyleVarScrollbarSize, 8)
	if imgui.BeginChildV("idle-body", v2(w+10, bodyH), false, 0) {
		renderIdleBody(since, w)
	}
	imgui.EndChild()
	imgui.PopStyleVar()
	g.PopStyleColorV(3)

	renderFooter(since, x0, origin.Y+winH-padBottom-footerH, w)
}

func renderIdleBody(since time.Time, w float32) {
	// Download location
	{
		p := imgui.CursorScreenPos()
		dy := rise(since, .11)
		x, y := p.X, p.Y+dy
		h := float32(76)
		fillRect(x, y, w, h, 8, colSurface)
		iconFolder(x+14, y+(h-20)/2, colMuted)

		btnW := buttonWidth("Open folder", iconExternal, 13) - 12
		tx, tw := x+46, w-46-btnW-28
		drawText(faceBold, 11, tx, y+12, colMuted, "DOWNLOAD LOCATION")
		drawText(faceMono, 12.5, tx, y+30, colText, ellipsize(faceMono, 12.5, EquicordDirectory, tw))
		hint := []span{
			{faceRegular, colMuted, "Set the"},
			{faceMono, colText, " EQUICORD_USER_DATA_DIR"},
			{faceRegular, colMuted, " environment variable to change this, then restart the installer."},
		}
		richText(hint, 12, tx, y+48, tw, 16, true)

		if button("open-folder", "Open folder", btnGhost, x+w-14-btnW, y+(h-32)/2, btnW, 32, true, iconExternal) {
			dir := path.Dir(EquicordDirectory)
			_ = os.MkdirAll(dir, 0o755)
			g.OpenURL("file://" + dir)
		}
		advance(p.X, p.Y, w, h)
		imgui.Dummy(v2(w, gap))
	}

	// Warnings
	if GithubError != nil && !IsDevInstall {
		notice(since, .18, w, colRed, []span{
			{faceSemiBold, colRedText, "Couldn't reach GitHub."},
			{faceRegular, colText, " Installing and repairing need Distra's latest release, so they're unavailable. Uninstalling still works. If this keeps happening, visit " + SupportUrl + "."},
		})
	}
	notice(since, .18, w, colWarn, []span{
		{faceSemiBold, colWarnText, "GitHub is the only official place to get Distra."},
		{faceRegular, colText, " Any other site or repository not hosted by mmeta or claiming to be us is malicious. If you downloaded from somewhere else, uninstall it immediately, run a malware scan, and change your Discord password."},
	})

	// Install list
	{
		p := imgui.CursorScreenPos()
		dy := rise(since, .25)
		drawText(faceBold, 12, p.X, p.Y+dy, colLabel, "SELECT AN INSTALL TO PATCH")
		advance(p.X, p.Y, w, 23)

		if len(discords) == 0 {
			msg := "No Discord installs found. Install Discord first, or point the installer at a custom location below."
			if runtime.GOOS == "linux" {
				msg += " Snap installs aren't supported."
			}
			q := imgui.CursorScreenPos()
			lines := wrapText(faceRegular, 13, msg, w)
			for i, line := range lines {
				drawText(faceRegular, 13, q.X, q.Y+dy+float32(i)*19, colMuted, line)
			}
			advance(q.X, q.Y, w, float32(len(lines))*19+8)
		}

		for i, d := range discords {
			di := d.(*DiscordInstall)
			name := strings.TrimPrefix(installName(di), "Discord ")
			installCard(since, i, name, di.path, di.isPatched, w, dy)
		}
		installCard(since, customChoiceIdx, "Custom install location", "Point the installer at any Discord folder", false, w, dy)

		if radioIdx == customChoiceIdx {
			customLocationField(since, w)
		}
	}

	// Leave room under the last card so a lifted, hovered card isn't clipped.
	imgui.Dummy(v2(w, 4))
	drawAlpha = 1
}

// notice draws a tinted callout with a warning icon, like the mockup's GitHub warning.
func notice(since time.Time, delay, w float32, tint color.RGBA, text []span) {
	p := imgui.CursorScreenPos()
	dy := rise(since, delay)
	x, y := p.X, p.Y+dy
	textW := w - 14 - 18 - 10 - 14
	h := richText(text, 13, 0, 0, textW, 19, false) + 22
	fillRect(x, y, w, h, 8, mix(colBackground, tint, .08))
	strokeRect(x, y, w, h, 8, withAlpha(tint, .35), 1)
	iconWarning(x+14, y+12, tint)
	richText(text, 13, x+14+18+10, y+11, textW, 19, true)
	advance(p.X, p.Y, w, h)
	imgui.Dummy(v2(w, gap))
}

func installCard(since time.Time, idx int, name, subtitle string, patched bool, w, dy float32) {
	p := imgui.CursorScreenPos()
	h := float32(58)
	selected := radioIdx == idx

	imgui.SetCursorScreenPos(p)
	if imgui.InvisibleButton("card-"+strconv.Itoa(idx), v2(w, h)) {
		radioIdx = idx
		keepAnimating(600 * time.Millisecond)
	}
	hovered := imgui.IsItemHovered()
	if hovered {
		imgui.SetMouseCursor(imgui.MouseCursorHand)
	}

	x, y := p.X, p.Y+dy-Ternary[float32](hovered, 1, 0)
	fillRect(x, y, w, h, 8, Ternary(hovered, colSurfaceHov, colSurface))
	if selected {
		strokeRect(x, y, w, h, 8, colAccent, 1)
	}

	// Radio
	cx, cy := x+14+10, y+h/2
	dl := drawList()
	dl.AddCircle(v2(cx, cy), 9, vec4(Ternary(selected, colAccent, colFaint)), 32, 2)
	if selected {
		dl.AddCircleFilled(v2(cx, cy), 5*pop(selectedAt(idx), 0), vec4(colAccent))
	}

	tx := x + 14 + 20 + 12
	drawText(faceSemiBold, 14, tx, y+11, colHeading, name)
	if patched {
		nw, _ := textSize(faceSemiBold, 14, name)
		bx := tx + nw + 8
		bw, _ := textSize(faceBold, 10, "PATCHED")
		fillRect(bx, y+12, bw+12, 16, 4, mix(colSurface, colGreen, .15))
		drawText(faceBold, 10, bx+6, y+13.5, colGreenText, "PATCHED")
	}
	drawText(faceMono, 12, tx, y+32, colMuted, ellipsize(faceMono, 12, subtitle, w-(tx-x)-14))

	advance(p.X, p.Y, w, h+8)
}

var (
	lastSelected   = -1
	lastSelectedAt time.Time
)

// selectedAt returns when idx became the selected install, for the radio dot's pop.
func selectedAt(idx int) time.Time {
	if lastSelected != idx {
		lastSelected, lastSelectedAt = idx, time.Now()
	}
	return lastSelectedAt
}

func customLocationField(since time.Time, w float32) {
	p := imgui.CursorScreenPos()
	x, y := p.X+32, p.Y
	fieldW := w - 32
	drawText(faceSemiBold, 12, x, y+2, colLabel, "Custom install location")

	candidates := makeAutoComplete()
	imgui.SetCursorScreenPos(v2(x, y+22))
	withFont(faceMono, 13, func() {
		g.PushStyleColor(g.StyleColorFrameBg, colDeep)
		g.PushStyleColor(g.StyleColorFrameBgHovered, colDeep)
		g.PushStyleColor(g.StyleColorFrameBgActive, colDeep)
		g.PushStyleColor(g.StyleColorBorder, Ternary(customFocused, colFocus, colDeep))
		g.PushStyleColor(g.StyleColorText, colText)
		g.PushStyleColor(g.StyleColorTextDisabled, colFaint)
		imgui.PushStyleVarFloat(imgui.StyleVarFrameRounding, 6)
		imgui.PushStyleVarFloat(imgui.StyleVarFrameBorderSize, 1)
		imgui.PushStyleVarVec2(imgui.StyleVarFramePadding, v2(12, 12))
		g.InputText(&customDir).
			Label("##custom-location").
			Hint(Ternary(runtime.GOOS == "windows", `C:\Path\To\Discord`, "/path/to/discord")).
			Size(fieldW).
			Flags(g.InputTextFlagsCallbackCompletion).
			OnChange(onCustomInputChanged).
			Callback(autoCompleteCallback(candidates)).
			Build()
		customFocused = imgui.IsItemActive()
		imgui.PopStyleVarV(3)
		g.PopStyleColorV(6)
	})

	h := float32(22 + 40 + 6)
	if customFocused && len(candidates) > 0 {
		shown := candidates[:min(len(candidates), 6)]
		line := "Tab completes: " + strings.Join(shown, "  ")
		drawText(faceMono, 12, x, y+h, colFaint, ellipsize(faceMono, 12, line, fieldW))
		h += 18
	}
	advance(p.X, p.Y, w, h+8)
}

// --- Footer ---

func renderFooter(since time.Time, x, y, w float32) {
	dy := rise(since, .32)
	y += dy
	drawList().AddLine(v2(x, y), v2(x+w, y), vec4(colBorder), 1)
	y += 17

	noTarget := radioIdx == customChoiceIdx && strings.TrimSpace(customDir) == ""
	canDownload := !noTarget && (GithubError == nil || IsDevInstall)

	isOpenAsar := false
	if radioIdx != customChoiceIdx && len(discords) > radioIdx {
		isOpenAsar = discords[radioIdx].(*DiscordInstall).IsOpenAsar()
	}
	openAsarLabel := Ternary(isOpenAsar, "Uninstall OpenAsar", "Install OpenAsar")

	bx := x
	place := func(label string, ic icon) (float32, float32) {
		bw := buttonWidth(label, ic, 14)
		left := bx
		bx += bw + 10
		return left, bw
	}

	if l, bw := place("Install", iconDownload); button("install", "Install", btnPrimary, l, y, bw, 38, canDownload, iconDownload) {
		run(opInstall)
	}
	if l, bw := place("Reinstall / Repair", nil); button("repair", "Reinstall / Repair", btnSecondary, l, y, bw, 38, canDownload, nil) {
		run(opRepair)
	}
	if l, bw := place(openAsarLabel, nil); button("openasar", openAsarLabel, btnGhost, l, y, bw, 38, !noTarget, nil) {
		handleOpenAsar()
	}
	uw := buttonWidth("Uninstall", nil, 14)
	if button("uninstall", "Uninstall", btnDanger, x+w-uw, y, uw, 38, !noTarget, nil) {
		run(opUninstall)
	}
	drawAlpha = 1
}

// --- Working screen ---

func renderWorking(since time.Time) {
	op := currentOp
	winW, winH := g.GetAvailableRegion()
	origin := imgui.CursorScreenPos()
	w := winW - 2*padX - 120
	x := origin.X + padX + 60

	step, doneAt := op.snapshot()
	stepsH := 24 + float32(len(op.steps))*26 + float32(len(op.steps)-1)*6
	total := float32(44+16+22+8+16) + 28 + 31 + 28 + stepsH
	y := origin.Y + padTop + (winH-padTop-padBottom-total)/2

	working, _, _ := op.titles()

	// Spinner, title and subtitle
	dy := rise(since, .04)
	cx, cy := x+w/2, y+dy+22
	t := float32(time.Since(since).Seconds())
	arc(cx, cy, 18, 0, 2*math.Pi, colBorder, 4)
	arc(cx, cy, 18, t*2*math.Pi-math.Pi/2, t*2*math.Pi, colAccent, 4)
	drawCentered(faceBold, 18, cx, y+dy+44+16, colHeading, working)
	drawCentered(faceRegular, 13, cx, y+dy+44+16+22+8, colMuted, "Working on "+op.name+". Keep Discord closed until this finishes.")
	y += 44 + 16 + 22 + 8 + 16 + 28

	// Progress bar
	dy = rise(since, .11)
	op.shownProgress += (op.progress() - op.shownProgress) * .12
	pct := int(math.Round(float64(op.shownProgress * 100)))
	label := op.steps[min(step, len(op.steps)-1)]
	drawText(faceSemiBold, 12, x, y+dy, colLabel, label)
	pctText := strconv.Itoa(pct) + "%"
	pw, _ := textSize(faceSemiBold, 12, pctText)
	drawText(faceSemiBold, 12, x+w-pw, y+dy, colLabel, pctText)
	by := y + dy + 23
	fillRect(x, by, w, 8, 4, colDeep)
	fillW := w * op.shownProgress
	if fillW > 1 {
		fillRect(x, by, fillW, 8, 4, colAccent)
		sheen(x, by, fillW, 8, since)
	}
	y += 31 + 28

	// Steps
	dy = rise(since, .18)
	fillRect(x, y+dy, w, stepsH, 8, colSurface)
	ry := y + dy + 12
	for i, s := range op.steps {
		ix, iy := x+14+8, ry+13
		switch {
		case i < step:
			iconCheckCircle(ix, iy, 8*pop(doneAt[i], 0), colGreen, 1, 1.8)
			drawText(faceRegular, 13, x+14+16+10, ry+5, colText, s)
		case i == step:
			pulse := .65 + .35*float32(math.Cos(time.Since(since).Seconds()*2*math.Pi/1.2))
			drawAlpha *= pulse
			drawList().AddCircle(v2(ix, iy), 7, vec4(colAccent), 24, 2)
			drawList().AddCircleFilled(v2(ix, iy), 3, vec4(colAccent))
			drawAlpha /= pulse
			drawText(faceRegular, 13, x+14+16+10, ry+5, colHeading, s)
		default:
			drawList().AddCircle(v2(ix, iy), 7, vec4(colSecondary), 24, 2)
			drawText(faceRegular, 13, x+14+16+10, ry+5, colFaint, s)
		}
		ry += 26 + 6
	}
}

// sheen sweeps a soft highlight across the progress bar fill.
func sheen(x, y, w, h float32, since time.Time) {
	period := 1.8
	t := float32(math.Mod(time.Since(since).Seconds(), period) / period)
	bandW := w * .35
	bx := x + (-1.2+t*4.4)*bandW
	imgui.PushClipRect(v2(x, y), v2(x+w, y+h), true)
	const slices = 12
	for i := 0; i < slices; i++ {
		f := (float32(i) + .5) / slices
		a := .22 * (1 - float32(math.Abs(float64(f*2-1))))
		sx := bx + bandW*float32(i)/slices
		fillRect(sx, y, bandW/slices+.5, h, 0, withAlpha(hex(0xffffff), a))
	}
	imgui.PopClipRect()
}

func drawCentered(face fontFace, size, cx, y float32, c color.RGBA, text string) {
	tw, _ := textSize(face, size, text)
	drawText(face, size, cx-tw/2, y, c, text)
}

// --- Done screen ---

func renderDone(since time.Time) {
	op := currentOp
	winW, winH := g.GetAvailableRegion()
	origin := imgui.CursorScreenPos()
	cx := origin.X + winW/2

	_, title, subtitle := op.titles()
	lines := wrapText(faceRegular, 13, subtitle, 420)
	subH := float32(len(lines)) * 19.5
	total := 72 + 24 + 27 + 8 + subH + 24 + 38
	y := origin.Y + padTop + (winH-padTop-padBottom-total)/2

	// Icon: pops in, then the check draws itself.
	scale := pop(since, 0)
	elapsed := float32(time.Since(since).Seconds())
	drawAlpha = clamp01(elapsed / .35)
	drawList().AddCircleFilled(v2(cx, y+36), 34*scale, vec4(mix(colBackground, colGreen, .14)))
	iconCheckCircle(cx, y+36, 26*scale, colGreen, (elapsed-.3)/.45, 4)
	y += 72 + 24

	dy := rise(since, .11)
	drawCentered(faceBold, 22, cx, y+dy, colHeading, title)
	y += 27 + 8
	for i, line := range lines {
		drawCentered(faceRegular, 13, cx, y+dy+float32(i)*19.5, colMuted, line)
	}
	y += subH + 24

	dy = rise(since, .18)
	rw, bw := buttonWidth("Restart Discord", nil, 14), buttonWidth("Back", nil, 14)
	bx := cx - (rw+10+bw)/2
	if button("restart", "Restart Discord", btnPrimary, bx, y+dy, rw, 38, true, nil) {
		di := op.target
		go func() {
			if err := restartDiscord(di); err != nil {
				ShowModal("Couldn't restart Discord", "Start "+installName(di)+" yourself to finish.", err.Error())
			}
		}()
	}
	if button("back", "Back", btnSecondary, bx+rw+10, y+dy, bw, 38, true, nil) {
		setScreen(screenIdle)
	}
	drawAlpha = 1
}

// --- Modals ---

type modalButton struct {
	label   string
	kind    buttonKind
	onClick func()
}

func closeButton(label string) modalButton {
	return modalButton{label, btnSecondary, func() {}}
}

func modals() g.Widget {
	return g.Custom(func() {
		openAsarAccept := modalButton{"Install OpenAsar", btnPrimary, func() {
			acceptedOpenAsar = true
			if di := getChosenInstall(); di != nil {
				startOperation(opOpenAsar, di)
			}
		}}

		modal("#scuffed-install", "Hold on!", "You have a broken Discord install. Sometimes Discord installs to the wrong location. "+
			"Fix this before patching, or Distra will likely not work.\n\n"+
			"Use the button below to jump there and delete any folder called Discord or Squirrel. If the folder is then empty, "+
			"delete it too. Then check that Discord still starts, and reinstall it if it doesn't.", "",
			modalButton{"Take me there", btnPrimary, func() {
				// this issue only exists on windows so using Windows specific path is oki
				g.OpenURL("file://" + path.Join(os.Getenv("PROGRAMDATA"), os.Getenv("USERNAME")))
			}}, closeButton("Close"))

		modal("#openasar-confirm", "Install OpenAsar?", "OpenAsar is an open-source alternative to Discord desktop's app.asar. "+
			"Distra is in no way affiliated with OpenAsar.\n\n"+
			"You're installing it at your own risk. If you run into problems with OpenAsar, no support will be provided here; "+
			"join the OpenAsar server instead.", "",
			openAsarAccept, closeButton("Cancel"))

		modal("#insufficient-permissions", "Insufficient permissions", "Permission denied. Please grant the installer permission in System Settings.", "",
			modalButton{"Open Settings", btnPrimary, func() {
				// "App Management" permissions doesnt exist on Monterey, just use full disk access for now
				g.OpenURL("x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles")
			}}, closeButton("Close"))

		modal("#invalid-custom-location", "Invalid location", "That folder isn't a valid Discord install. Make sure you select its base folder.\n\n"+
			"Discord from Snap isn't supported; use the Flatpak or .deb instead.", "", closeButton("OK"))

		modal("#modal"+strconv.Itoa(modalId), modalTitle, modalMessage, modalExtra, closeButton("OK"))

		modal("#update-prompt", "An installer update is available", "Distra Installer will download the new version, which can take a moment, "+
			"then reopen itself.\n\nOn macOS, updating opens the download in your browser instead.", "",
			modalButton{"Update now", btnPrimary, func() {
				if runtime.GOOS == "darwin" {
					g.OpenURL(GetInstallerDownloadLink())
					return
				}
				if err := UpdateSelf(); err != nil {
					ShowModal("Failed to update the installer", "Please download the latest Distra Installer manually.", err.Error())
				} else if err = RelaunchSelf(); err != nil {
					ShowModal("Failed to restart the installer", "Please reopen Distra Installer manually.", err.Error())
				}
			}}, closeButton("Later"))
	})
}

// modal draws a themed popup. Buttons close it after running their action.
func modal(id, title, body, extra string, buttons ...modalButton) {
	const w = float32(460)
	g.PushStyleColor(g.StyleColorPopupBg, colBackground)
	g.PushStyleColor(g.StyleColorBorder, colBorder)
	g.PushStyleColor(g.StyleColorModalWindowDimBg, withAlpha(hex(0), .6))
	imgui.PushStyleVarFloat(imgui.StyleVarWindowRounding, 10)
	imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, v2(24, 22))
	defer func() {
		imgui.PopStyleVarV(2)
		g.PopStyleColorV(3)
	}()

	if !imgui.BeginPopupModalV(id, nil, imgui.WindowFlagsNoTitleBar|imgui.WindowFlagsAlwaysAutoResize|imgui.WindowFlagsNoMove) {
		return
	}
	defer imgui.EndPopup()

	p := imgui.CursorScreenPos()
	x, y := p.X, p.Y
	drawText(faceBold, 18, x, y, colHeading, title)
	y += 22 + 10
	for _, line := range wrapText(faceRegular, 13, body, w) {
		drawText(faceRegular, 13, x, y, colText, line)
		y += 19
	}
	if extra != "" {
		lines := wrapText(faceMono, 12, extra, w-24)
		lines = lines[:min(len(lines), 12)]
		h := float32(len(lines))*17 + 20
		y += 10
		fillRect(x, y, w, h, 6, colDeep)
		for i, line := range lines {
			drawText(faceMono, 12, x+12, y+10+float32(i)*17, colMuted, line)
		}
		y += h
	}
	y += 20

	bx := x + w
	for i := len(buttons) - 1; i >= 0; i-- {
		b := buttons[i]
		bw := buttonWidth(b.label, nil, 14)
		bx -= bw
		if button(id+"-btn-"+strconv.Itoa(i), b.label, b.kind, bx, y, bw, 38, true, nil) {
			imgui.CloseCurrentPopup()
			b.onClick()
		}
		bx -= 10
	}
	advance(x, p.Y, w, y+38-p.Y)
}
