// Command icons draws the dashboard's icons (story #35) into web/public:
// a favicon per status, the home screen icons and favicon.ico.
//
// The mark is the halo: a disc with a ring around it, drawn from the status
// circle. Run it from the repository root after changing the mark:
//
//	go run ./tools/icons
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

// The mark on a 32×32 grid, as in the design canvas.
const (
	discR      = 9.5
	ringR      = 14.0
	ringStroke = 3.0
)

// tone is a favicon colour: light for light tab bars, dark for dark ones.
type tone struct{ name, light, dark string }

// tones are the status colours of the circle (web/src/styles.css). The dark
// tints keep the favicon readable on dark tab bars.
var tones = []tone{
	{"healthy", "#0B6B45", "#3FB27F"},
	{"warning", "#A15C07", "#E8A33D"},
	{"error", "#B42318", "#F0645A"},
	{"unreachable", "#5B6472", "#9AA1AC"}, // also starting and loading
	{"stopped", "#7A818C", "#6B727D"},
	{"busy", "#101318", "#C9CDD3"}, // a node action is running
}

const (
	ink       = "#101318"
	iconMark  = "#3FB27F" // the green halo on the ink home screen icon
	iconShare = 0.6       // the mark's share of the icon: inside the maskable safe zone
)

func main() {
	out := filepath.Join("web", "public")
	must(os.MkdirAll(filepath.Join(out, "icons"), 0o755))

	for _, t := range tones {
		write(filepath.Join(out, "icons", "favicon-"+t.name+".svg"), []byte(faviconSVG(t)))
	}
	for _, size := range []int{180, 192, 512} {
		name := fmt.Sprintf("icon-%d.png", size)
		if size == 180 {
			name = "apple-touch-icon.png"
		}
		write(filepath.Join(out, "icons", name), encodePNG(appIcon(size)))
	}
	// favicon.ico for browsers that ask for it: the neutral grey halo.
	ico := encodePNG(mark(32, 32.0/32, parse(tones[3].light), color.RGBA{}))
	write(filepath.Join(out, "favicon.ico"), icoFile(ico, 32))
}

// faviconSVG is the halo in one status colour, switching to the dark tint
// when the browser is dark.
func faviconSVG(t tone) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">`+
		`<style>.d{fill:%[1]s}.r{fill:none;stroke:%[1]s}`+
		`@media (prefers-color-scheme:dark){.d{fill:%[2]s}.r{stroke:%[2]s}}</style>`+
		`<circle class="d" cx="16" cy="16" r="%[3]g"/>`+
		`<circle class="r" cx="16" cy="16" r="%[4]g" stroke-width="%[5]g"/></svg>`+"\n",
		t.light, t.dark, discR, ringR, ringStroke)
}

// appIcon is the home screen icon: the green halo on ink, edge to edge, so
// phones can round or mask the corners themselves.
func appIcon(size int) image.Image {
	return mark(size, float64(size)*iconShare/32, parse(iconMark), parse(ink))
}

// mark draws the halo centred on a size×size image, scale pixels per grid
// unit, with 4×4 supersampling for smooth edges.
func mark(size int, scale float64, fg, bg color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	c := float64(size) / 2
	const n = 4
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			hits := 0
			for sy := 0; sy < n; sy++ {
				for sx := 0; sx < n; sx++ {
					px := (float64(x)+(float64(sx)+0.5)/n-c)/scale + 16
					py := (float64(y)+(float64(sy)+0.5)/n-c)/scale + 16
					if inMark(px, py) {
						hits++
					}
				}
			}
			img.SetRGBA(x, y, blend(fg, bg, float64(hits)/(n*n)))
		}
	}
	return img
}

func inMark(x, y float64) bool {
	d := math.Hypot(x-16, y-16)
	return d <= discR || math.Abs(d-ringR) <= ringStroke/2
}

// blend puts fg over bg with coverage a; a transparent bg gives a
// transparent pixel outside the mark.
func blend(fg, bg color.RGBA, a float64) color.RGBA {
	if bg.A == 0 {
		return color.RGBA{fg.R, fg.G, fg.B, uint8(math.Round(a * 255))}
	}
	mix := func(f, b uint8) uint8 { return uint8(math.Round(float64(f)*a + float64(b)*(1-a))) }
	return color.RGBA{mix(fg.R, bg.R), mix(fg.G, bg.G), mix(fg.B, bg.B), 255}
}

// icoFile wraps one PNG in an ICO container.
func icoFile(pngData []byte, size int) []byte {
	var b bytes.Buffer
	le := func(v any) { must(binary.Write(&b, binary.LittleEndian, v)) }
	le([3]uint16{0, 1, 1})                       // reserved, type icon, one image
	le([4]uint8{uint8(size), uint8(size), 0, 0}) // width, height, palette, reserved
	le([2]uint16{1, 32})                         // colour planes, bits per pixel
	le([2]uint32{uint32(len(pngData)), 6 + 16})  // data size, offset
	b.Write(pngData)
	return b.Bytes()
}

func encodePNG(img image.Image) []byte {
	var b bytes.Buffer
	must(png.Encode(&b, img))
	return b.Bytes()
}

func parse(hex string) color.RGBA {
	var r, g, b uint8
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		log.Fatalf("colour %q: %v", hex, err)
	}
	return color.RGBA{r, g, b, 255}
}

func write(path string, data []byte) {
	must(os.WriteFile(path, data, 0o644))
	fmt.Println("wrote", path)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
