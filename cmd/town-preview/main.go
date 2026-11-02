// Command town-preview draws the town as TownView draws its still parts (ground in file order, props by
// depth with a bottom-centre pivot, venues' outsides and shut doors), 32 px a tile, from town.json,
// limezu-assets.txt and the LimeZu files (docs/town-v2.md): a design aid, seconds instead of a Unity build.
// No walkers, cars or animation; Unity is still the judge.
//
//	go run ./cmd/town-preview REPO LIMEZU_DIRS OUT.png [x0 y0 w h]   tiles; default: the camera's frame
//	go run ./cmd/town-preview bbox SHEET.png x y w h                the opaque blocks inside a sheet's rectangle
//
// LIMEZU_DIRS: ':'-separated roots, the first that has a file wins (the project's imported Assets/LimeZu,
// then the whole pack). TOWN=draft.json draws a draft instead of the repo's town.json.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type piece struct {
	Key        string
	X, Y, W, H float64
	Frames     int
}

func (p *piece) UnmarshalJSON(b []byte) error {
	var m struct {
		Key    string  `json:"key"`
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		W      float64 `json:"w"`
		H      float64 `json:"h"`
		Frames int     `json:"frames"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*p = piece{m.Key, m.X, m.Y, m.W, m.H, m.Frames}
	return nil
}

type town struct {
	W      float64                `json:"w"`
	H      float64                `json:"h"`
	Focus  struct{ X, Y float64 } `json:"focus"`
	Frame  struct{ X, Y float64 } `json:"frame"`
	Ground []piece                `json:"ground"`
	Props  []piece                `json:"props"`
	Venues []struct {
		X        float64 `json:"x"`
		Y        float64 `json:"y"`
		Exterior []piece `json:"exterior"`
		Doorway  *piece  `json:"doorway"`
	} `json:"venues"`
}

const T = 32

var assets = map[string][2]string{} // key -> path, rect
var cache = map[string]image.Image{}

func load(limezu, key string, frames int) image.Image {
	if im, ok := cache[key]; ok {
		return im
	}
	a, ok := assets[key]
	if !ok {
		fmt.Fprintln(os.Stderr, "no asset", key)
		cache[key] = nil
		return nil
	}
	var f *os.File
	var err error
	for _, root := range strings.Split(limezu, ":") { // the project's imported files first, then the whole pack
		if f, err = os.Open(filepath.Join(root, a[0])); err == nil {
			break
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "missing", key, a[0])
		cache[key] = nil
		return nil
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		cache[key] = nil
		return nil
	}
	if a[1] != "-" && a[1] != "" {
		var r [4]int
		for i, s := range strings.Split(a[1], ",") {
			r[i], _ = strconv.Atoi(strings.TrimSpace(s))
		}
		sub := image.NewRGBA(image.Rect(0, 0, r[2], r[3]))
		draw.Draw(sub, sub.Bounds(), im, image.Pt(r[0], r[1]), draw.Src)
		im = sub
	}
	if frames > 1 {
		b := im.Bounds()
		sub := image.NewRGBA(image.Rect(0, 0, b.Dx()/frames, b.Dy()))
		draw.Draw(sub, sub.Bounds(), im, b.Min, draw.Src)
		im = sub
	}
	cache[key] = im
	return im
}

func main() {
	if len(os.Args) == 7 && os.Args[1] == "bbox" {
		bbox(os.Args[2:])
		return
	}
	if len(os.Args) != 4 && len(os.Args) != 8 {
		fmt.Fprintln(os.Stderr, "usage: town-preview REPO LIMEZU_DIRS OUT.png [x0 y0 w h] | bbox SHEET.png x y w h")
		os.Exit(2)
	}
	repo, limezu, out := os.Args[1], os.Args[2], os.Args[3]
	lists := []string{filepath.Join(repo, "unity/AgentPaymentAuthority/limezu-assets.txt")}
	if e := os.Getenv("ASSETS"); e != "" { // a draft's own lines, besides the project's
		lists = append(lists, e)
	}
	for _, l := range lists {
		readAssets(l)
	}
	tp := filepath.Join(repo, "unity/AgentPaymentAuthority/Assets/APA/Resources/Town/town.json")
	if e := os.Getenv("TOWN"); e != "" { // a draft beside the repo's
		tp = e
	}
	raw, _ := os.ReadFile(tp)
	var t town
	if err := json.Unmarshal(raw, &t); err != nil {
		panic(err)
	}
	x0, y0, w, h := t.Focus.X-t.Frame.X/2, t.Focus.Y-t.Frame.Y/2, t.Frame.X, t.Frame.Y
	if len(os.Args) == 8 {
		x0, _ = strconv.ParseFloat(os.Args[4], 64)
		y0, _ = strconv.ParseFloat(os.Args[5], 64)
		w, _ = strconv.ParseFloat(os.Args[6], 64)
		h, _ = strconv.ParseFloat(os.Args[7], 64)
	}
	dst := image.NewRGBA(image.Rect(0, 0, int(w*T), int(h*T)))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{color.RGBA{16, 19, 26, 255}}, image.Point{}, draw.Src)
	// put draws im with its bottom left at town (x, y), tiled over (pw, ph) tiles when pw > 0.
	put := func(im image.Image, x, y, pw, ph float64) {
		if im == nil {
			return
		}
		b := im.Bounds()
		iw, ih := float64(b.Dx())/T, float64(b.Dy())/T
		if pw <= 0 {
			pw, ph = iw, ih
		}
		if ph <= 0 {
			ph = ih
		}
		for ty := 0.0; ty < ph-1e-6; ty += ih {
			for tx := 0.0; tx < pw-1e-6; tx += iw {
				cw, chh := math.Min(iw, pw-tx), math.Min(ih, ph-ty)
				px := int(math.Round((x + tx - x0) * T))
				py := int(math.Round((y0 + h - (y + ty + chh)) * T))
				src := image.Rect(b.Min.X, b.Max.Y-int(math.Round(chh*T)), b.Min.X+int(math.Round(cw*T)), b.Max.Y)
				draw.Draw(dst, image.Rect(px, py, px+src.Dx(), py+src.Dy()), im, src.Min, draw.Over)
			}
		}
	}
	for _, g := range t.Ground {
		put(load(limezu, g.Key, g.Frames), g.X, g.Y, g.W, g.H)
	}
	type item struct {
		order int
		f     func()
	}
	var items []item
	ord := func(y float64) int { return 2000 - int(math.Round(y*20)) }
	for _, p := range t.Props {
		p := p
		items = append(items, item{ord(p.Y), func() {
			im := load(limezu, p.Key, p.Frames)
			if im == nil {
				return
			}
			iw := float64(im.Bounds().Dx()) / T
			if p.W > 0 {
				iw = p.W
			}
			put(im, p.X-iw/2, p.Y, p.W, p.H)
		}})
	}
	for _, v := range t.Venues {
		v := v
		bottom := math.MaxFloat64
		for _, e := range v.Exterior {
			bottom = math.Min(bottom, e.Y)
		}
		for i, e := range v.Exterior {
			e := e
			items = append(items, item{ord(v.Y+bottom) + i, func() { put(load(limezu, e.Key, e.Frames), v.X+e.X, v.Y+e.Y, e.W, e.H) }})
		}
		if d := v.Doorway; d != nil {
			items = append(items, item{ord(v.Y+bottom) + len(v.Exterior), func() { put(load(limezu, d.Key, d.Frames), v.X+d.X, v.Y+d.Y, 0, 0) }})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].order < items[j].order })
	for _, it := range items {
		it.f()
	}
	o, _ := os.Create(out)
	png.Encode(o, dst)
	o.Close()
	fmt.Println("wrote", out, dst.Bounds().Dx(), "x", dst.Bounds().Dy())
}

// bbox prints the opaque blocks of a sheet inside a rectangle, as x,y,w,h: rows split at fully transparent
// lines, then columns within each row band.
func bbox(a []string) {
	f, err := os.Open(a[0])
	if err != nil {
		panic(err)
	}
	img, err := png.Decode(f)
	if err != nil {
		panic(err)
	}
	n := func(i int) int { v, _ := strconv.Atoi(a[i]); return v }
	r := image.Rect(n(1), n(2), n(1)+n(3), n(2)+n(4)).Intersect(img.Bounds())
	op := func(x, y int) bool { _, _, _, al := img.At(x, y).RGBA(); return al > 0 }
	on := func(x0, x1, y0, y1 int) bool {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if op(x, y) {
					return true
				}
			}
		}
		return false
	}
	for y := r.Min.Y; y < r.Max.Y; {
		if !on(r.Min.X, r.Max.X, y, y+1) {
			y++
			continue
		}
		y0 := y
		for y < r.Max.Y && on(r.Min.X, r.Max.X, y, y+1) {
			y++
		}
		for x := r.Min.X; x < r.Max.X; {
			if !on(x, x+1, y0, y) {
				x++
				continue
			}
			x0 := x
			for x < r.Max.X && on(x, x+1, y0, y) {
				x++
			}
			fmt.Printf("%d,%d,%d,%d\n", x0, y0, x-x0, y-y0)
		}
	}
}

// readAssets adds a limezu-assets.txt's lines: key, path, rect ("-" the whole file).
func readAssets(path string) {
	af, err := os.Open(path)
	if err != nil {
		return
	}
	defer af.Close()
	sc := bufio.NewScanner(af)
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "#") || l == "" {
			continue
		}
		f := strings.Split(l, "\t")
		if len(f) >= 3 {
			assets[f[0]] = [2]string{f[1], f[2]}
		}
	}
}
