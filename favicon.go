// Package favicon is a collage plugin that makes a site's icons from one source
// image and links them from every page's head.
//
//	app, err := collage.New(&collage.Config{
//		Plugins: []collage.Plugin{favicon.New(favicon.Options{
//			Source:     "assets/icon.png",
//			SVG:        "assets/icon.svg",
//			Name:       "The blog",
//			ThemeColor: "#0f172a",
//		})},
//	})
//
// From a square PNG, ideally 512 pixels or more, it makes favicon.ico (16, 32 and
// 48 pixels), apple-touch-icon.png (180), icon-192.png and icon-512.png, and a
// site.webmanifest listing them; an SVG given beside it is served as icon.svg for
// the browsers that prefer one. Everything is made once, when the application
// starts, and served at the root as static documents, so a static build writes
// them like any other file. Every page's head gets the matching <link> and
// <meta name="theme-color"> tags through {{hoist "head"}}.
//
// A source that cannot be decoded, or is far from square, stops the application
// from starting: a site whose icon silently failed looks fine to everyone but the
// person looking at a browser tab.
package favicon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"image"
	"image/color"
	_ "image/gif" // registered for image.Decode: a source may be any of these
	_ "image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/Elagoht/collage/pkg/collage"
	"golang.org/x/image/draw"
)

// Name is the plugin's name, and the key its configuration is found under.
const Name = "elagoht/favicon"

// Options configures the plugin.
type Options struct {
	// Source is the path of the source image: a PNG (or JPEG, or GIF), square,
	// ideally 512 pixels or larger. It is read from FS when FS is set, and from
	// disk, relative to the working directory, otherwise. Ignored when Image is
	// set.
	Source string `json:"source"`
	// SVG is the path of an SVG version of the icon, read the way Source is and
	// served unchanged as /icon.svg. Optional: an SVG is not rasterised, so it is
	// never a substitute for Source.
	SVG string `json:"svg"`
	// FS is where Source and SVG are read from: an embed.FS, say, so the binary
	// runs from any directory. Nil reads them from disk.
	FS fs.FS `json:"-"`
	// Image and SVGImage are the files' bytes, for an application that already
	// has them; each takes the place of its path.
	Image    []byte `json:"-"`
	SVGImage []byte `json:"-"`

	// Name and ShortName are the site's name in the manifest: what an installed
	// app is called, and the shorter label under its icon. ShortName defaults to
	// Name.
	Name      string `json:"name"`
	ShortName string `json:"shortName"`
	// ThemeColor colours the browser's interface around the site, in the head's
	// <meta name="theme-color"> and the manifest. BackgroundColor is the splash
	// screen's colour while an installed app starts, and what the Apple touch
	// icon's transparent pixels are filled with — iOS fills them with black
	// otherwise. Each is a CSS hex colour, "#0f172a", or a colour name; empty
	// leaves it out.
	ThemeColor      string `json:"themeColor"`
	BackgroundColor string `json:"backgroundColor"`
	// Display is the manifest's display mode. Default "standalone".
	Display string `json:"display"`
	// StartURL is the page an installed app opens on. Default "/".
	StartURL string `json:"startUrl"`
	// NoManifest leaves out site.webmanifest, and its <link>.
	NoManifest bool `json:"noManifest"`
}

// Sizes favicon.ico holds.
var icoSizes = []int{16, 32, 48}

// minSource is the size below which a source is upscaled for icon-512.png, and
// warned about.
const minSource = 512

// Errors Init returns for a source it cannot use.
var (
	// ErrNoSource is returned when neither Source nor Image is set.
	ErrNoSource = errors.New("favicon: a source image is required: set Source or Image")
	// ErrNotSquare is returned for a source whose sides differ by more than a
	// tenth: made square, it would be squashed or mostly empty.
	ErrNotSquare = errors.New("favicon: the source image is not square")
	// ErrBadSVG is returned for an SVG that does not hold an <svg> element.
	ErrBadSVG = errors.New("favicon: the SVG is not an SVG")
)

// Plugin makes and serves the icons.
type Plugin struct {
	opts Options
	// tags is what every page's head is given, assembled once at Init.
	tags []hoisted
}

type hoisted struct {
	key  string
	html template.HTML
}

// New returns a plugin with opts as its starting point, which the application's
// own configuration is then decoded over.
func New(opts Options) *Plugin { return &Plugin{opts: opts} }

func (p *Plugin) Name() string                   { return Name }
func (p *Plugin) Version() string                { return "0.1.4" }
func (p *Plugin) Shutdown(context.Context) error { return nil }

var (
	_ collage.Plugin           = (*Plugin)(nil)
	_ collage.BeforeRenderHook = (*Plugin)(nil)
)

// Init reads the configuration, makes every icon, and registers each as a
// document at the root.
func (p *Plugin) Init(_ context.Context, host collage.Host) error {
	cfg, err := collage.PluginConfig(host, p.opts)
	if err != nil {
		return err
	}
	p.opts = cfg
	o := &p.opts
	if o.Display == "" {
		o.Display = "standalone"
	}
	if o.StartURL == "" {
		o.StartURL = "/"
	}
	if o.ShortName == "" {
		o.ShortName = o.Name
	}
	for _, c := range []struct{ name, value string }{{"ThemeColor", o.ThemeColor}, {"BackgroundColor", o.BackgroundColor}} {
		if c.value != "" && !cssColor.MatchString(c.value) {
			return fmt.Errorf("favicon: %s %q is neither a hex colour nor a colour name", c.name, c.value)
		}
	}

	raw, err := p.read(o.Image, o.Source)
	if err != nil {
		return err
	}
	if raw == nil {
		return ErrNoSource
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("favicon: the source image cannot be decoded: %w", err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 || abs(w-h)*10 > max(w, h) {
		return fmt.Errorf("%w: it is %dx%d", ErrNotSquare, w, h)
	}
	if min(w, h) < minSource {
		host.Logger().Warn("favicon: the source image is smaller than 512 pixels, so the larger icons are upscaled and blurred",
			"width", w, "height", h)
	}
	svg, err := p.read(o.SVGImage, o.SVG)
	if err != nil {
		return err
	}
	if svg != nil && !bytes.Contains(svg, []byte("<svg")) {
		return ErrBadSVG
	}

	files, err := p.render(src, svg)
	if err != nil {
		return err
	}
	// The hrefs carry a digest of the source: a browser keeps a favicon far longer
	// than any Cache-Control asks it to, and a new name is the one thing it does
	// not argue with. The documents ignore the query, so the old links still work.
	sum := sha256.Sum256(append(append([]byte(nil), raw...), svg...))
	version := "?v=" + hex.EncodeToString(sum[:4])
	if !o.NoManifest {
		manifest, err := p.manifest(version, svg != nil)
		if err != nil {
			return err
		}
		files = append(files, file{"/site.webmanifest", "application/manifest+json", manifest})
	}
	for _, f := range files {
		doc := collage.NewDocument(Name+":"+strings.TrimPrefix(f.path, "/"), f.contentType).
			AtRoot(f.path).
			WithBody(f.body).
			WithCacheParams().
			Static().
			Build()
		if err := host.RegisterDocument(doc); err != nil {
			return fmt.Errorf("favicon: %w", err)
		}
	}
	p.tags = p.head(version, svg != nil)
	return nil
}

// read returns data when it is set, and otherwise the file at name, from FS or
// from disk. Neither is nil, not an error: the caller decides whether the file
// was required.
func (p *Plugin) read(data []byte, name string) ([]byte, error) {
	if data != nil {
		return data, nil
	}
	if name == "" {
		return nil, nil
	}
	var (
		raw []byte
		err error
	)
	if p.opts.FS != nil {
		raw, err = fs.ReadFile(p.opts.FS, name)
	} else {
		raw, err = os.ReadFile(name)
	}
	if err != nil {
		return nil, fmt.Errorf("favicon: %w", err)
	}
	return raw, nil
}

type file struct {
	path, contentType string
	body              []byte
}

// render makes every image file.
func (p *Plugin) render(src image.Image, svg []byte) ([]file, error) {
	var entries [][]byte
	for _, size := range icoSizes {
		b, err := encodePNG(resize(src, size, nil))
		if err != nil {
			return nil, err
		}
		entries = append(entries, b)
	}
	files := []file{{"/favicon.ico", "image/x-icon", encodeICO(icoSizes, entries)}}

	var bg color.Color
	if c, ok := parseHex(p.opts.BackgroundColor); ok {
		bg = c
	}
	for _, f := range []struct {
		path string
		size int
		bg   color.Color
	}{
		{"/apple-touch-icon.png", 180, bg},
		{"/icon-192.png", 192, nil},
		{"/icon-512.png", 512, nil},
	} {
		b, err := encodePNG(resize(src, f.size, f.bg))
		if err != nil {
			return nil, err
		}
		files = append(files, file{f.path, "image/png", b})
	}
	if svg != nil {
		files = append(files, file{"/icon.svg", "image/svg+xml", svg})
	}
	return files, nil
}

// resize scales src to a size×size square with Catmull-Rom, the sharpest of
// x/image's filters that does not ring visibly at icon sizes. A source that is
// not quite square is centred, not stretched. bg, when set, fills what the image
// leaves transparent.
func resize(src image.Image, size int, bg color.Color) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	if bg != nil {
		draw.Draw(dst, dst.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	}
	b := src.Bounds()
	w, h := size, size
	if b.Dx() > b.Dy() {
		h = size * b.Dy() / b.Dx()
	} else if b.Dy() > b.Dx() {
		w = size * b.Dx() / b.Dy()
	}
	x, y := (size-w)/2, (size-h)/2
	draw.CatmullRom.Scale(dst, image.Rect(x, y, x+w, y+h), src, b, draw.Over, nil)
	return dst
}

func encodePNG(img image.Image) ([]byte, error) {
	var b bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&b, img); err != nil {
		return nil, fmt.Errorf("favicon: %w", err)
	}
	return b.Bytes(), nil
}

// encodeICO writes an ICO file whose entries are PNG images, which every browser
// since Internet Explorer 9 reads, and which is a fraction of the size of the
// uncompressed bitmaps the format began with.
func encodeICO(sizes []int, images [][]byte) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	// ICONDIR: reserved, type 1 (icon), count.
	_ = binary.Write(&b, le, [3]uint16{0, 1, uint16(len(images))})
	offset := 6 + 16*len(images)
	for i, img := range images {
		dim := uint8(sizes[i]) // 256 would be written as 0; no entry here is that large
		// ICONDIRENTRY: width, height, palette size, reserved, colour planes,
		// bits per pixel, size of the data, where it starts.
		b.Write([]byte{dim, dim, 0, 0})
		_ = binary.Write(&b, le, [2]uint16{1, 32})
		_ = binary.Write(&b, le, [2]uint32{uint32(len(img)), uint32(offset)})
		offset += len(img)
	}
	for _, img := range images {
		b.Write(img)
	}
	return b.Bytes()
}

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose,omitempty"`
}

type manifest struct {
	Name            string         `json:"name,omitempty"`
	ShortName       string         `json:"short_name,omitempty"`
	StartURL        string         `json:"start_url"`
	Display         string         `json:"display"`
	ThemeColor      string         `json:"theme_color,omitempty"`
	BackgroundColor string         `json:"background_color,omitempty"`
	Icons           []manifestIcon `json:"icons"`
}

func (p *Plugin) manifest(version string, svg bool) ([]byte, error) {
	o := p.opts
	m := manifest{
		Name:            o.Name,
		ShortName:       o.ShortName,
		StartURL:        o.StartURL,
		Display:         o.Display,
		ThemeColor:      o.ThemeColor,
		BackgroundColor: o.BackgroundColor,
		Icons: []manifestIcon{
			{Src: "/icon-192.png" + version, Sizes: "192x192", Type: "image/png"},
			{Src: "/icon-512.png" + version, Sizes: "512x512", Type: "image/png"},
		},
	}
	if svg {
		m.Icons = append(m.Icons, manifestIcon{Src: "/icon.svg" + version, Sizes: "any", Type: "image/svg+xml"})
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("favicon: %w", err)
	}
	return append(body, '\n'), nil
}

// head assembles the tags every page is given. Every value in them is either
// fixed here or checked by cssColor, which admits nothing that could close an
// attribute; they are escaped all the same.
func (p *Plugin) head(version string, svg bool) []hoisted {
	esc := template.HTMLEscapeString
	tags := []hoisted{{"favicon:ico", template.HTML(`<link rel="icon" href="/favicon.ico` + version + `" sizes="32x32">`)}}
	if svg {
		tags = append(tags, hoisted{"favicon:svg", template.HTML(`<link rel="icon" href="/icon.svg` + version + `" type="image/svg+xml">`)})
	}
	tags = append(tags, hoisted{"favicon:apple", template.HTML(`<link rel="apple-touch-icon" href="/apple-touch-icon.png` + version + `">`)})
	if !p.opts.NoManifest {
		tags = append(tags, hoisted{"favicon:manifest", template.HTML(`<link rel="manifest" href="/site.webmanifest` + version + `">`)})
	}
	if p.opts.ThemeColor != "" {
		tags = append(tags, hoisted{"favicon:theme-color", template.HTML(`<meta name="theme-color" content="` + esc(p.opts.ThemeColor) + `">`)})
	}
	return tags
}

// OnBeforeRender puts the icons' tags into the page's head. Hoisted at depth
// zero, so a page can replace one by declaring the same key — a section with a
// theme colour of its own.
func (p *Plugin) OnBeforeRender(_ context.Context, ev *collage.BeforeRenderEvent) error {
	if ev.Context == nil {
		return nil
	}
	for _, t := range p.tags {
		ev.Context.Hoist("head", t.key, t.html)
	}
	return nil
}

// cssColor admits a hex colour or a colour name: what a manifest and a
// theme-color meta tag take, and nothing that could leave the attribute it is
// written into.
var cssColor = regexp.MustCompile(`^(#([0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})|[a-zA-Z]+)$`)

// parseHex reads a #rgb or #rrggbb colour. A colour name is not parsed — the
// Apple touch icon is then left transparent — because a table of CSS names is
// more than the one icon it would fill is worth.
func parseHex(s string) (color.Color, bool) {
	s, ok := strings.CutPrefix(s, "#")
	if !ok {
		return nil, false
	}
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return nil, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return nil, false
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}, true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
