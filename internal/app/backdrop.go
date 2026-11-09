package app

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// backdropFade is how long another song's backdrop takes to fade in.
const backdropFade = 700 * time.Millisecond

// backdropCache holds the static background and the previous track's fade.
type backdropCache struct {
	canvas, before *ui.Canvas
	fadeAt         time.Time
	art            *artwork
	tint           ui.Color
}

// backdrop redraws only on resize or a cover change.
func (a *app) backdrop(c *ui.Context, art *artwork, tint ui.Color) {
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		cw, ch := int(r.W/8)+1, int(r.H/8)+1
		bd := &a.backdropCache
		if bd.canvas != nil {
			if w, h := bd.canvas.Size(); w != cw || h != ch {
				bd.canvas, bd.before = nil, nil
			}
		}
		now := p.Now()
		if bd.canvas != nil && (bd.art != art || bd.tint != tint) {
			bd.canvas, bd.before, bd.fadeAt = bd.before, bd.canvas, now
		}
		if bd.canvas == nil || bd.art != art || bd.tint != tint {
			if bd.canvas == nil {
				bd.canvas = ui.NewCanvas(cw, ch)
			}
			bd.art, bd.tint = art, tint
			bd.canvas.Draw(p, func(cp *ui.Painter) {
				drawBackdrop(cp, ui.Rect{W: float32(cw), H: float32(ch)}, art, tint)
			})
		}
		p.Image(bd.canvas.Bitmap(), r, ui.FillBox)
		if bd.before != nil {
			if k := float32(now.Sub(bd.fadeAt)) / float32(backdropFade); k < 1 {
				k = 1 - (1-k)*(1-k)*(1-k)
				p.ImageRegion(bd.before.Bitmap(), ui.Rect{}, r, ui.ImageOptions{Tint: white.Alpha(1 - k)})
				p.AnimationFrame()
			} else {
				bd.before = nil
			}
		}
	})
}

// drawBackdrop paints the still backdrop in r.
func drawBackdrop(p *ui.Painter, r ui.Rect, art *artwork, tint ui.Color) {
	top, foot := backdropColors(tint)
	p.FillGradient(r, ui.LinearGradient{Angle: 180, Oklab: true, Stops: []ui.GradientStop{
		{Color: top, At: 0}, {Color: foot, At: 1},
	}}, 0)
	if art != nil && art.backdrop != nil {
		// The cover, blurred, as a faint wash of its own colors.
		p.ImageRegion(art.backdrop, ui.Rect{W: 48, H: 48}, coverRect(r, 48, 48), ui.ImageOptions{Tint: white.Alpha(0.1)})
	}
	// Darker toward the foot, under the controls.
	p.FillGradient(r, ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
		{Color: ui.RGBA(0, 0, 0, 0), At: 0.55}, {Color: ui.RGBA(0, 0, 0, 0.18), At: 1},
	}}, 0)
}

// coverRect returns where an image of w×h goes to cover r, centered.
func coverRect(r ui.Rect, w, h float32) ui.Rect {
	s := max(r.W/w, r.H/h)
	return ui.Rect{X: r.X + (r.W-w*s)/2, Y: r.Y + (r.H-h*s)/2, W: w * s, H: h * s}
}

// backdropColors returns the top and the foot of the backdrop's gradient
// for a cover of tint: its hue, deep and rich, at the same depth whatever
// the cover, so that white reads over it.
// A gray cover stays gray.
func backdropColors(tint ui.Color) (top, foot ui.Color) {
	h, s, _ := hsv(float64(tint.R)/255, float64(tint.G)/255, float64(tint.B)/255)
	if s > 0.12 {
		s = min(max(s*1.3, 0.45), 0.66)
	}
	return hsvColor(h, s, 0.35), hsvColor(h, min(s*1.05, 0.7), 0.24)
}

// hsvColor is the color of hue h (degrees), saturation s and value v.
func hsvColor(h, s, v float64) ui.Color {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	var r, g, b float64
	switch {
	case h < 60:
		r, g = c, x
	case h < 120:
		r, g = x, c
	case h < 180:
		g, b = c, x
	case h < 240:
		g, b = x, c
	case h < 300:
		r, b = x, c
	default:
		r, b = c, x
	}
	m := v - c
	return ui.RGB(uint8((r+m)*255+0.5), uint8((g+m)*255+0.5), uint8((b+m)*255+0.5))
}

// backdropOver paints the backdrop again over band, a strip of win, the
// box the backdrop fills, as opaque at each height in band as alpha says
// for it, from 0 at its top to 1 at its foot: what moves under the strip
// fades into the backdrop with no edge, the very pixels around it where
// it is opaque.
func (a *app) backdropOver(p *ui.Painter, win, band ui.Rect, alpha func(t float32) float32) {
	bd := &a.backdropCache
	if bd.canvas == nil || win.W <= 0 || win.H <= 0 || band.H <= 0 {
		return
	}
	cw, ch := bd.canvas.Size()
	sx, sy := float32(cw)/win.W, float32(ch)/win.H
	// Its song's fading in over the song before's, as the backdrop.
	before := float32(0)
	if bd.before != nil {
		if k := float32(p.Now().Sub(bd.fadeAt)) / float32(backdropFade); k < 1 {
			before = (1 - k) * (1 - k) * (1 - k)
		}
	}
	const strip = 2
	for y := float32(0); y < band.H; y += strip {
		o := alpha((y + strip/2) / band.H)
		if o <= 0.004 {
			continue
		}
		dst := ui.Rect{X: band.X, Y: band.Y + y, W: band.W, H: min(strip, band.H-y)}
		src := ui.Rect{X: (dst.X - win.X) * sx, Y: (dst.Y - win.Y) * sy, W: dst.W * sx, H: dst.H * sy}
		p.ImageRegion(bd.canvas.Bitmap(), src, dst, ui.ImageOptions{Tint: white.Alpha(o)})
		if before > 0 {
			p.ImageRegion(bd.before.Bitmap(), src, dst, ui.ImageOptions{Tint: white.Alpha(o * before)})
		}
	}
}
