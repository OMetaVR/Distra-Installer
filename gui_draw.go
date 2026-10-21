//go:build !cli

/*
 * SPDX-License-Identifier: GPL-3.0
 * Distra Installer, a fork of the Equicord and Vencord installers
 * Copyright (c) 2026 Vendicated, Equicord and Distra contributors
 */

package main

// Drawing helpers for the Distra Installer GUI: theme colors, fonts, animation timing and the custom widgets
// (buttons, cards, icons, wrapped text) everything in gui.go is built from.

import (
	_ "embed"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"

	g "github.com/AllenDang/giu"
	"github.com/AllenDang/imgui-go"
)

//go:embed fonts/Inter-Regular.ttf
var fontRegularBytes []byte

//go:embed fonts/Inter-SemiBold.ttf
var fontSemiBoldBytes []byte

//go:embed fonts/Inter-Bold.ttf
var fontBoldBytes []byte

//go:embed fonts/IBMPlexMono-Regular.ttf
var fontMonoBytes []byte

// Theme, taken from the installer mockup.
var (
	colBackground = hex(0x313338)
	colSurface    = hex(0x2b2d31)
	colSurfaceHov = hex(0x383a40)
	colDeep       = hex(0x1e1f22)
	colBorder     = hex(0x3f4147)
	colText       = hex(0xdbdee1)
	colHeading    = hex(0xf2f3f5)
	colLabel      = hex(0xb5bac1)
	colMuted      = hex(0xa5abb4)
	colFaint      = hex(0x80848e)
	colSecondary  = hex(0x4e5058)
	colSecondHov  = hex(0x6d6f78)
	colAccent     = hex(0x5865f2)
	colAccentHov  = hex(0x4c57d0)
	colFocus      = hex(0x00a8fc)
	colGreen      = hex(0x23a559)
	colGreenText  = hex(0x3dd27a)
	colWarn       = hex(0xf0b232)
	colWarnText   = hex(0xf5d08a)
	colRed        = hex(0xda373c)
	colRedText    = hex(0xf58f91)
)

type fontFace int

const (
	faceRegular fontFace = iota
	faceSemiBold
	faceBold
	faceMono
)

type fontKey struct {
	face fontFace
	size float32
}

var fonts = map[fontKey]*g.FontInfo{}

// loadFonts registers every font face and size the GUI uses. Must run before the master window is created.
// Sizes are CSS font sizes, as in the mockup. Dear ImGui sizes a font by its line height (ascent + descent)
// instead of its em, so each size is scaled by that font's line height in ems.
func loadFonts() {
	const interLineHeight, monoLineHeight = 1.21, 1.3
	g.SetDefaultFontFromBytes(fontRegularBytes, 13*interLineHeight)
	for _, f := range []struct {
		face       fontFace
		name       string
		data       []byte
		lineHeight float32
		sizes      []float32
	}{
		{faceRegular, "Inter", fontRegularBytes, interLineHeight, []float32{12, 13}},
		{faceSemiBold, "Inter SemiBold", fontSemiBoldBytes, interLineHeight, []float32{12, 13, 14}},
		{faceBold, "Inter Bold", fontBoldBytes, interLineHeight, []float32{10, 11, 12, 18, 22}},
		{faceMono, "IBM Plex Mono", fontMonoBytes, monoLineHeight, []float32{12, 12.5, 13}},
	} {
		for _, size := range f.sizes {
			fonts[fontKey{f.face, size}] = g.AddFontFromBytes(f.name, f.data, size*f.lineHeight)
		}
	}
}

// withFont runs fn with the given font pushed. Text measured or drawn inside uses it.
func withFont(face fontFace, size float32, fn func()) {
	if f := fonts[fontKey{face, size}]; f != nil && g.PushFont(f) {
		defer g.PopFont()
	}
	fn()
}

// textSize measures text in the given font. imgui-go's CalcTextSize also measures the string's trailing NUL,
// which adds a missing glyph's width, so that is subtracted.
func textSize(face fontFace, size float32, text string) (w, h float32) {
	if text == "" {
		return 0, 0
	}
	withFont(face, size, func() {
		var nul float32
		w, h = g.CalcTextSize(text)
		nul, _ = g.CalcTextSize("")
		w -= nul
	})
	return
}

func hex(v uint32) color.RGBA {
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func withAlpha(c color.RGBA, a float32) color.RGBA {
	c.A = uint8(float32(c.A) * clamp01(a))
	return c
}

func mix(a, b color.RGBA, t float32) color.RGBA {
	t = clamp01(t)
	lerp := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t) }
	return color.RGBA{lerp(a.R, b.R), lerp(a.G, b.G), lerp(a.B, b.B), lerp(a.A, b.A)}
}

func clamp01(v float32) float32 {
	return float32(math.Max(0, math.Min(1, float64(v))))
}

// drawAlpha fades everything drawn while it is below 1. Used for the entrance animations.
var drawAlpha float32 = 1

func vec4(c color.RGBA) imgui.Vec4 {
	return imgui.Vec4{X: float32(c.R) / 255, Y: float32(c.G) / 255, Z: float32(c.B) / 255, W: float32(c.A) / 255 * drawAlpha}
}

func v2(x, y float32) imgui.Vec2 { return imgui.Vec2{X: x, Y: y} }

func drawList() imgui.DrawList { return imgui.GetWindowDrawList() }

func fillRect(x, y, w, h, r float32, c color.RGBA) {
	drawList().AddRectFilled(v2(x, y), v2(x+w, y+h), vec4(c), r, 0)
}

func strokeRect(x, y, w, h, r float32, c color.RGBA, thickness float32) {
	drawList().AddRect(v2(x+.5, y+.5), v2(x+w-.5, y+h-.5), vec4(c), r, 0, thickness)
}

func drawText(face fontFace, size, x, y float32, c color.RGBA, text string) {
	withFont(face, size, func() {
		g.GetCanvas().AddText(image.Pt(int(math.Round(float64(x))), int(math.Round(float64(y)))), withAlpha(c, drawAlpha), text)
	})
}

// --- Animation ---

var (
	animMu         sync.Mutex
	animatingUntil time.Time
	animTicker     sync.Once
)

// keepAnimating asks for continuous redraws for at least d, so time based animations keep moving.
func keepAnimating(d time.Duration) {
	animMu.Lock()
	if until := time.Now().Add(d); until.After(animatingUntil) {
		animatingUntil = until
	}
	animMu.Unlock()
	animTicker.Do(func() {
		go func() {
			for range time.Tick(time.Second / 60) {
				animMu.Lock()
				active := time.Now().Before(animatingUntil)
				animMu.Unlock()
				if active {
					g.Update()
				}
			}
		}()
	})
}

func easeOut(t float32) float32 {
	t = clamp01(t)
	return 1 - float32(math.Pow(float64(1-t), 3))
}

// rise reproduces the mockup's entrance: fade in while moving up 10px, staggered by delay seconds.
// It returns the vertical offset to draw with and sets drawAlpha.
func rise(since time.Time, delay float32) float32 {
	t := easeOut((float32(time.Since(since).Seconds()) - delay) / .55)
	drawAlpha = t
	return (1 - t) * 10
}

// pop reproduces the mockup's pop: scale from .5 overshooting to 1.12 then settling at 1.
func pop(since time.Time, delay float32) float32 {
	t := clamp01((float32(time.Since(since).Seconds()) - delay) / .5)
	if t < .7 {
		return .5 + easeOut(t/.7)*.62
	}
	return 1.12 - (t-.7)/.3*.12
}

// --- Widgets ---

type buttonKind int

const (
	btnPrimary buttonKind = iota
	btnSecondary
	btnGhost
	btnDanger
)

type icon func(x, y float32, c color.RGBA)

// buttonWidth is the width a button needs for its label and optional icon.
func buttonWidth(label string, ic icon, size float32) float32 {
	w, _ := textSize(faceSemiBold, size, label)
	w += 36
	if ic != nil {
		w += 24
	}
	return w
}

// button draws a mockup style button at x, y and reports whether it was clicked.
func button(id, label string, kind buttonKind, x, y, w, h float32, enabled bool, ic icon) bool {
	fontSize := float32(14)
	if h < 36 {
		fontSize = 13
	}
	imgui.SetCursorScreenPos(v2(x, y))
	clicked := imgui.InvisibleButton(id, v2(w, h)) && enabled
	hovered := enabled && imgui.IsItemHovered()
	active := enabled && imgui.IsItemActive()
	if hovered {
		imgui.SetMouseCursor(imgui.MouseCursorHand)
		if !active {
			y--
		}
	}

	saved := drawAlpha
	if !enabled {
		drawAlpha *= .45
	}
	defer func() { drawAlpha = saved }()

	textColor := hex(0xffffff)
	switch kind {
	case btnPrimary:
		fillRect(x, y, w, h, 6, Ternary(hovered, colAccentHov, colAccent))
	case btnSecondary:
		fillRect(x, y, w, h, 6, Ternary(hovered, colSecondHov, colSecondary))
	case btnGhost:
		textColor = colText
		if hovered {
			fillRect(x, y, w, h, 6, colSurfaceHov)
		}
		strokeRect(x, y, w, h, 6, colSecondary, 1)
	case btnDanger:
		if hovered {
			fillRect(x, y, w, h, 6, colRed)
		} else {
			textColor = colRedText
			strokeRect(x, y, w, h, 6, withAlpha(colRed, .7), 1)
		}
	}

	tw, th := textSize(faceSemiBold, fontSize, label)
	contentW := tw
	if ic != nil {
		contentW += 24
	}
	cx := x + (w-contentW)/2
	if ic != nil {
		ic(cx, y+(h-16)/2, textColor)
		cx += 24
	}
	drawText(faceSemiBold, fontSize, cx, y+(h-th)/2, textColor, label)
	return clicked
}

// wrapText splits text into lines no wider than width.
func wrapText(face fontFace, size float32, text string, width float32) []string {
	space := spaceWidth(face, size)
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		line, lineW := "", float32(0)
		for _, word := range strings.Fields(para) {
			w, _ := textSize(face, size, word)
			if line != "" && lineW+space+w > width {
				lines = append(lines, line)
				line, lineW = "", 0
			}
			if line != "" {
				line += " "
				lineW += space
			}
			line += word
			lineW += w
		}
		lines = append(lines, line)
	}
	return lines
}

// span is a run of text in one font and color, for paragraphs that mix bold and regular text.
type span struct {
	face  fontFace
	color color.RGBA
	text  string
}

// richText lays out spans word by word, wrapping at width. It draws when draw is true and returns the height.
func richText(spans []span, size, x, y, width, lineHeight float32, draw bool) float32 {
	cx, cy := x, y
	space := spaceWidth(faceRegular, size)
	for _, s := range spans {
		leadingSpace := strings.TrimLeft(s.text, " \n") != s.text
		for i, word := range strings.Fields(s.text) {
			w, _ := textSize(s.face, size, word)
			gap := Ternary(cx > x && (i > 0 || leadingSpace), space, 0)
			if cx > x && cx+gap+w > x+width {
				cx, cy, gap = x, cy+lineHeight, 0
			}
			cx += gap
			if draw {
				drawText(s.face, size, cx, cy+(lineHeight-size*1.2)/2, s.color, word)
			}
			cx += w
		}
	}
	return cy - y + lineHeight
}

// spaceWidth measures a space between letters; imgui reports a lone " " far wider than it draws.
func spaceWidth(face fontFace, size float32) float32 {
	with, _ := textSize(face, size, "a a")
	without, _ := textSize(face, size, "aa")
	return with - without
}

// ellipsize shortens text with "..." so it fits width.
func ellipsize(face fontFace, size float32, text string, width float32) string {
	if w, _ := textSize(face, size, text); w <= width {
		return text
	}
	runes := []rune(text)
	for n := len(runes) - 1; n > 0; n-- {
		candidate := string(runes[:n]) + "..."
		if w, _ := textSize(face, size, candidate); w <= width {
			return candidate
		}
	}
	return "..."
}

// --- Icons, drawn at 16x16 unless noted ---

func iconDownload(x, y float32, c color.RGBA) {
	dl, col := drawList(), vec4(c)
	dl.AddLine(v2(x+8, y+2.7), v2(x+8, y+10), col, 1.8)
	dl.PathClear()
	dl.PathLineTo(v2(x+5.3, y+7.3))
	dl.PathLineTo(v2(x+8, y+10))
	dl.PathLineTo(v2(x+10.7, y+7.3))
	dl.PathStroke(col, false, 1.8)
	dl.AddLine(v2(x+3.3, y+13.3), v2(x+12.7, y+13.3), col, 1.8)
}

func iconExternal(x, y float32, c color.RGBA) {
	dl, col := drawList(), vec4(c)
	x, y = x+1, y+1
	dl.PathClear()
	dl.PathLineTo(v2(x+8, y+2))
	dl.PathLineTo(v2(x+12, y+2))
	dl.PathLineTo(v2(x+12, y+6))
	dl.PathStroke(col, false, 1.4)
	dl.AddLine(v2(x+12, y+2), v2(x+6.5, y+7.5), col, 1.4)
	dl.PathClear()
	dl.PathLineTo(v2(x+11, y+8.5))
	dl.PathLineTo(v2(x+11, y+12))
	dl.PathLineTo(v2(x+2, y+12))
	dl.PathLineTo(v2(x+2, y+3))
	dl.PathLineTo(v2(x+5.5, y+3))
	dl.PathStroke(col, false, 1.4)
}

// iconFolder is 20x20.
func iconFolder(x, y float32, c color.RGBA) {
	dl, col := drawList(), vec4(c)
	s := float32(20) / 24
	p := func(px, py float32) imgui.Vec2 { return v2(x+px*s, y+py*s) }
	dl.PathClear()
	dl.PathLineTo(p(3, 7))
	dl.PathLineTo(p(3.6, 5.6))
	dl.PathLineTo(p(5, 5))
	dl.PathLineTo(p(9, 5))
	dl.PathLineTo(p(11, 7))
	dl.PathLineTo(p(19, 7))
	dl.PathLineTo(p(20.4, 7.6))
	dl.PathLineTo(p(21, 9))
	dl.PathLineTo(p(21, 18))
	dl.PathLineTo(p(20.4, 19.4))
	dl.PathLineTo(p(19, 20))
	dl.PathLineTo(p(5, 20))
	dl.PathLineTo(p(3.6, 19.4))
	dl.PathLineTo(p(3, 18))
	dl.PathStroke(col, true, 1.5)
}

// iconWarning is 18x18.
func iconWarning(x, y float32, c color.RGBA) {
	dl, col := drawList(), vec4(c)
	s := float32(18) / 24
	p := func(px, py float32) imgui.Vec2 { return v2(x+px*s, y+py*s) }
	dl.PathClear()
	dl.PathLineTo(p(12, 3))
	dl.PathLineTo(p(22, 21))
	dl.PathLineTo(p(2, 21))
	dl.PathStroke(col, true, 1.6)
	dl.AddLine(p(12, 10), p(12, 15), col, 1.6)
	dl.AddCircleFilled(p(12, 18), 1.2*s+.4, col)
}

// iconCheckCircle is a filled green circle with a white check, radius r around cx, cy.
// progress (0-1) draws the check partially, for the done screen's draw-in animation.
func iconCheckCircle(cx, cy, r float32, fill color.RGBA, progress float32, thickness float32) {
	dl := drawList()
	dl.AddCircleFilled(v2(cx, cy), r, vec4(fill))
	// Check mark points relative to a 72px icon centered at 36,36, scaled to r (26 for that icon).
	s := r / 26
	pts := []imgui.Vec2{v2(cx-12*s, cy+1*s), v2(cx-3.5*s, cy+9.5*s), v2(cx+13*s, cy-8*s)}
	strokePartial(pts, progress, hex(0xffffff), thickness)
}

// strokePartial strokes a polyline up to progress of its total length.
func strokePartial(pts []imgui.Vec2, progress float32, c color.RGBA, thickness float32) {
	if progress <= 0 {
		return
	}
	total := float32(0)
	for i := 1; i < len(pts); i++ {
		total += dist(pts[i-1], pts[i])
	}
	remaining := total * clamp01(progress)
	dl := drawList()
	dl.PathClear()
	dl.PathLineTo(pts[0])
	for i := 1; i < len(pts) && remaining > 0; i++ {
		seg := dist(pts[i-1], pts[i])
		if seg <= remaining {
			dl.PathLineTo(pts[i])
			remaining -= seg
			continue
		}
		t := remaining / seg
		dl.PathLineTo(v2(pts[i-1].X+(pts[i].X-pts[i-1].X)*t, pts[i-1].Y+(pts[i].Y-pts[i-1].Y)*t))
		remaining = 0
	}
	dl.PathStroke(vec4(c), false, thickness)
}

func dist(a, b imgui.Vec2) float32 {
	return float32(math.Hypot(float64(b.X-a.X), float64(b.Y-a.Y)))
}

// arc strokes a circular arc from angle a0 to a1 (radians).
func arc(cx, cy, r, a0, a1 float32, c color.RGBA, thickness float32) {
	dl := drawList()
	dl.PathClear()
	dl.PathArcTo(v2(cx, cy), r, a0, a1, 32)
	dl.PathStroke(vec4(c), false, thickness)
}
