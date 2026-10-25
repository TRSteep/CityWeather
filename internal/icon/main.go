// Command icon generates the original CityWeather artwork and multi-size ICO.
// Run from the repository root: go run ./internal/icon
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

type rgb struct{ r, g, b float64 }

func circle(x, y, cx, cy, radius float64) bool { return math.Hypot(x-cx, y-cy) <= radius }
func rounded(x, y, left, top, right, bottom, radius float64) bool {
	cx := math.Max(left+radius, math.Min(x, right-radius))
	cy := math.Max(top+radius, math.Min(y, bottom-radius))
	return circle(x, y, cx, cy, radius)
}

// Geometry is expressed on a 256-unit canvas. Supersampling keeps small sizes sharp.
func artwork(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const samples = 4
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var sum rgb
			count := 0.0
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/samples) * 256 / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/samples) * 256 / float64(size)
					if !rounded(x, y, 8, 8, 248, 248, 54) {
						continue
					}
					blend := (x + y) / 512
					c := rgb{22 - 10*blend, 104 - 38*blend, 193 - 45*blend}
					// Sun with eight thick, rounded rays.
					for ray := 0; ray < 8; ray++ {
						angle := float64(ray) * math.Pi / 4
						cx, cy := 98+57*math.Cos(angle), 91+57*math.Sin(angle)
						if circle(x, y, cx, cy, 7) {
							c = rgb{255, 200, 64}
						}
					}
					if circle(x, y, 98, 91, 39) {
						c = rgb{255, 193, 56}
					}
					// Soft shadow below the white cloud.
					if rounded(x, y, 45, 143, 209, 191, 24) {
						c = rgb{12, 66, 133}
					}
					if circle(x, y, 77, 153, 32) || circle(x, y, 123, 129, 42) || circle(x, y, 169, 150, 33) || rounded(x, y, 75, 149, 178, 184, 12) {
						c = rgb{242, 249, 255}
					}
					// Thermometer: navy border, white glass, warm red mercury.
					if rounded(x, y, 174, 119, 208, 189, 17) || circle(x, y, 191, 191, 26) {
						c = rgb{13, 65, 119}
					}
					if rounded(x, y, 181, 126, 201, 189, 10) || circle(x, y, 191, 191, 19) {
						c = rgb{250, 252, 255}
					}
					if rounded(x, y, 187, 146, 195, 192, 4) || circle(x, y, 191, 191, 12) {
						c = rgb{239, 91, 74}
					}
					sum.r += c.r
					sum.g += c.g
					sum.b += c.b
					count++
				}
			}
			if count > 0 {
				img.SetNRGBA(px, py, color.NRGBA{uint8(sum.r / count), uint8(sum.g / count), uint8(sum.b / count), uint8(255 * count / (samples * samples))})
			}
		}
	}
	return img
}

// A 32-bit DIB with an AND transparency mask works with Windows and rsrc.
func dib(img *image.NRGBA) []byte {
	size := img.Bounds().Dx()
	maskStride := ((size + 31) / 32) * 4
	var out bytes.Buffer
	put := func(v any) {
		if err := binary.Write(&out, binary.LittleEndian, v); err != nil {
			log.Fatal(err)
		}
	}
	put(uint32(40))
	put(int32(size))
	put(int32(size * 2))
	put(uint16(1))
	put(uint16(32))
	put(uint32(0))
	put(uint32(size * size * 4))
	put(int32(0))
	put(int32(0))
	put(uint32(0))
	put(uint32(0))
	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			c := img.NRGBAAt(x, y)
			out.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	for y := size - 1; y >= 0; y-- {
		mask := make([]byte, maskStride)
		for x := 0; x < size; x++ {
			if img.NRGBAAt(x, y).A == 0 {
				mask[x/8] |= 0x80 >> uint(x%8)
			}
		}
		out.Write(mask)
	}
	return out.Bytes()
}

func main() {
	if err := os.MkdirAll("assets", 0755); err != nil {
		log.Fatal(err)
	}
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	var icon bytes.Buffer
	put := func(v any) {
		if err := binary.Write(&icon, binary.LittleEndian, v); err != nil {
			log.Fatal(err)
		}
	}
	put(uint16(0))
	put(uint16(1))
	put(uint16(len(sizes)))
	images := make([][]byte, len(sizes))
	offset := 6 + 16*len(sizes)
	for i, size := range sizes {
		images[i] = dib(artwork(size))
		put(uint8(size % 256))
		put(uint8(size % 256))
		put(uint8(0))
		put(uint8(0))
		put(uint16(1))
		put(uint16(32))
		put(uint32(len(images[i])))
		put(uint32(offset))
		offset += len(images[i])
	}
	for _, data := range images {
		icon.Write(data)
	}
	if err := os.WriteFile("assets/app.ico", icon.Bytes(), 0644); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create("assets/app.png")
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(f, artwork(256)); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}
