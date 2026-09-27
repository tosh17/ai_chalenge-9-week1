package mcpsrv

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// DiagramTools рисует блок-схему реальных вызовов.
func DiagramTools() []Tool {
	return []Tool{{
		Name:        "draw_flow",
		Description: "Рисует блок-схему последовательности вызовов MCP. Шаги передаёт оркестратор по фактическому следу, не модель.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"steps":{"type":"array","items":{"type":"object","properties":{"server":{"type":"string"},"tool":{"type":"string"},"detail":{"type":"string"}}}}},"required":["steps"]}`),
		Handle: func(args json.RawMessage) (string, error) {
			var in flowInput
			if err := json.Unmarshal(args, &in); err != nil {
				return "", err
			}
			path, err := drawFlow(in)
			if err != nil {
				return "", err
			}
			raw, err := json.Marshal(map[string]any{
				"saved_path": path,
				"image_url":  "/media/" + filepath.Base(path),
			})
			if err != nil {
				return "", err
			}
			return string(raw), nil
		},
	}}
}

type flowInput struct {
	Title string     `json:"title"`
	Steps []flowStep `json:"steps"`
}

type flowStep struct {
	Server string `json:"server"`
	Tool   string `json:"tool"`
	Detail string `json:"detail"`
}

func drawFlow(in flowInput) (string, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "Ход запроса"
	}
	steps := in.Steps
	if len(steps) == 0 {
		steps = []flowStep{{Server: "—", Tool: "вызовов не было"}}
	}
	face, err := loadFace(18)
	if err != nil {
		return "", err
	}
	small, err := loadFace(14)
	if err != nil {
		return "", err
	}
	width := 880
	boxH := 78
	gap := 28
	top := 70
	height := top + len(steps)*boxH + (len(steps)-1)*gap + 40
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	fill(img, color.RGBA{0xF6, 0xF3, 0xEC, 0xFF})
	drawString(img, face, 36, 42, title, color.RGBA{0x1C, 0x1A, 0x17, 0xFF})

	colors := map[string]color.RGBA{
		"поиск":   {0x2F, 0x6F, 0x4E, 0xFF},
		"перевод": {0x3D, 0x5A, 0x80, 0xFF},
		"погода":  {0xC4, 0x5C, 0x26, 0xFF},
		"схема":   {0x6B, 0x70, 0x5C, 0xFF},
	}
	for i, step := range steps {
		y := top + i*(boxH+gap)
		c := colors[step.Server]
		if c.A == 0 {
			c = color.RGBA{0x5C, 0x56, 0x4E, 0xFF}
		}
		rect(img, 36, y, width-72, boxH, c)
		label := fmt.Sprintf("%d. %s · %s", i+1, step.Server, step.Tool)
		drawString(img, face, 52, y+30, label, color.White)
		if step.Detail != "" {
			drawString(img, small, 52, y+54, trimRunes(step.Detail, 90), color.RGBA{0xF6, 0xF3, 0xEC, 0xFF})
		}
		if i < len(steps)-1 {
			mid := width / 2
			line(img, mid, y+boxH, mid, y+boxH+gap, color.RGBA{0x1C, 0x1A, 0x17, 0xFF})
		}
	}

	dir := filepath.Join("data", "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("flow-%d.png", time.Now().UnixNano()))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return abs, nil
}

func loadFace(size float64) (font.Face, error) {
	for _, path := range []string{
		"/System/Library/Fonts/Supplemental/Arial.ttf",
		"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
		"/Library/Fonts/Arial.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		ft, err := opentype.Parse(raw)
		if err != nil {
			continue
		}
		face, err := opentype.NewFace(ft, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			continue
		}
		return face, nil
	}
	return nil, fmt.Errorf("не найден шрифт с кириллицей")
}

func fill(img *image.RGBA, c color.RGBA) {
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func rect(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	for yy := y; yy < y+h && yy < img.Bounds().Dy(); yy++ {
		for xx := x; xx < x+w && xx < img.Bounds().Dx(); xx++ {
			if xx >= 0 && yy >= 0 {
				img.SetRGBA(xx, yy, c)
			}
		}
	}
}

func line(img *image.RGBA, x1, y1, x2, y2 int, c color.RGBA) {
	if y2 < y1 {
		y1, y2 = y2, y1
	}
	for y := y1; y <= y2; y++ {
		img.SetRGBA(x1, y, c)
		if x1+1 < img.Bounds().Dx() {
			img.SetRGBA(x1+1, y, c)
		}
	}
}

func drawString(img *image.RGBA, face font.Face, x, y int, text string, col color.Color) {
	d := font.Drawer{Dst: img, Src: image.NewUniform(col), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(text)
}
