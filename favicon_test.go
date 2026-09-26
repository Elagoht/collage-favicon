package favicon_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	favicon "github.com/Elagoht/collage-favicon"
	"github.com/Elagoht/collage/pkg/collage"
)

// source makes a w×h PNG: opaque red with a transparent top-left quarter, so a
// test can see whether transparency was kept or filled.
func source(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if x >= w/2 || y >= h/2 {
				img.Set(x, y, color.NRGBA{R: 255, A: 255})
			}
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><rect width="1" height="1"/></svg>`

func site(t *testing.T, cfg *collage.Config, p *favicon.Plugin) *collage.App {
	t.Helper()
	cfg.Server = collage.ServerConfig{Host: "localhost", Port: 3000}
	cfg.Template = collage.TemplateConfig{FS: fstest.MapFS{
		"t/p.html": {Data: []byte(`<html><head>{{hoist "head"}}</head><body>x</body></html>`)},
	}, Root: "t"}
	cfg.Plugins = []collage.Plugin{p}
	app, err := collage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterPage(collage.NewPage("home").WithContent(collage.NewFragment("home", "p.html").Build()).WithPath("en", "/").Build()); err != nil {
		t.Fatal(err)
	}
	return app
}

func get(app *collage.App, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func decodePNG(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	return img
}

func TestIcons(t *testing.T) {
	app := site(t, &collage.Config{}, favicon.New(favicon.Options{Image: source(t, 512, 512)}))
	for _, c := range []struct {
		path, ctype string
		size        int
	}{
		{"/apple-touch-icon.png", "image/png", 180},
		{"/icon-192.png", "image/png", 192},
		{"/icon-512.png", "image/png", 512},
	} {
		rec := get(app, c.path)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != c.ctype {
			t.Fatalf("GET %s = %d %q", c.path, rec.Code, rec.Header().Get("Content-Type"))
		}
		if b := decodePNG(t, rec.Body.Bytes()).Bounds(); b.Dx() != c.size || b.Dy() != c.size {
			t.Errorf("%s is %v, want %d square", c.path, b, c.size)
		}
	}
	// A request carrying the version query the head links is the same file.
	if rec := get(app, "/icon-192.png?v=0123abcd"); rec.Code != http.StatusOK {
		t.Errorf("GET with ?v= = %d", rec.Code)
	}
	if rec := get(app, "/icon.svg"); rec.Code != http.StatusNotFound {
		t.Errorf("without an SVG, /icon.svg = %d", rec.Code)
	}
}

// favicon.ico is an ICO file whose three entries are PNGs of the sizes its
// directory claims.
func TestICO(t *testing.T) {
	app := site(t, &collage.Config{}, favicon.New(favicon.Options{Image: source(t, 600, 600)}))
	rec := get(app, "/favicon.ico")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/x-icon" {
		t.Fatalf("GET /favicon.ico = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	ico := rec.Body.Bytes()
	le := binary.LittleEndian
	if le.Uint16(ico[0:]) != 0 || le.Uint16(ico[2:]) != 1 || le.Uint16(ico[4:]) != 3 {
		t.Fatalf("ICONDIR = % x", ico[:6])
	}
	for i, want := range []int{16, 32, 48} {
		e := ico[6+16*i:]
		if int(e[0]) != want || int(e[1]) != want || le.Uint16(e[6:]) != 32 {
			t.Errorf("entry %d = % x", i, e[:16])
		}
		size, offset := le.Uint32(e[8:]), le.Uint32(e[12:])
		img := decodePNG(t, ico[offset:offset+size])
		if b := img.Bounds(); b.Dx() != want || b.Dy() != want {
			t.Errorf("entry %d holds %v, want %d", i, b, want)
		}
	}
}

func TestManifestAndHead(t *testing.T) {
	app := site(t, &collage.Config{}, favicon.New(favicon.Options{
		Image: source(t, 512, 512), SVGImage: []byte(svg),
		Name: "The blog", ThemeColor: "#0f172a", BackgroundColor: "#fff",
	}))
	rec := get(app, "/site.webmanifest")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("GET /site.webmanifest = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var m struct {
		Name            string `json:"name"`
		ShortName       string `json:"short_name"`
		StartURL        string `json:"start_url"`
		Display         string `json:"display"`
		ThemeColor      string `json:"theme_color"`
		BackgroundColor string `json:"background_color"`
		Icons           []struct{ Src, Sizes, Type string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "The blog" || m.ShortName != "The blog" || m.StartURL != "/" || m.Display != "standalone" ||
		m.ThemeColor != "#0f172a" || m.BackgroundColor != "#fff" || len(m.Icons) != 3 {
		t.Errorf("manifest = %+v", m)
	}
	if !strings.HasPrefix(m.Icons[0].Src, "/icon-192.png?v=") || m.Icons[0].Sizes != "192x192" ||
		m.Icons[2].Type != "image/svg+xml" || m.Icons[2].Sizes != "any" {
		t.Errorf("icons = %+v", m.Icons)
	}
	if rec := get(app, "/icon.svg"); rec.Body.String() != svg || rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Errorf("/icon.svg = %q %q", rec.Header().Get("Content-Type"), rec.Body.String())
	}

	page := get(app, "/").Body.String()
	version := m.Icons[0].Src[len("/icon-192.png"):]
	for _, want := range []string{
		`<link rel="icon" href="/favicon.ico` + version + `" sizes="32x32">`,
		`<link rel="icon" href="/icon.svg` + version + `" type="image/svg+xml">`,
		`<link rel="apple-touch-icon" href="/apple-touch-icon.png` + version + `">`,
		`<link rel="manifest" href="/site.webmanifest` + version + `">`,
		`<meta name="theme-color" content="#0f172a">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("head lacks %s\n%s", want, page)
		}
	}
}

// The Apple touch icon's transparent pixels take the background colour, since
// iOS would paint them black; the other icons keep their transparency.
func TestAppleIconBackground(t *testing.T) {
	app := site(t, &collage.Config{}, favicon.New(favicon.Options{Image: source(t, 512, 512), BackgroundColor: "#00ff00"}))
	apple := decodePNG(t, get(app, "/apple-touch-icon.png").Body.Bytes())
	if r, g, b, a := apple.At(5, 5).RGBA(); r != 0 || g != 0xffff || b != 0 || a != 0xffff {
		t.Errorf("apple corner = %d %d %d %d, want opaque green", r, g, b, a)
	}
	icon := decodePNG(t, get(app, "/icon-192.png").Body.Bytes())
	if _, _, _, a := icon.At(5, 5).RGBA(); a != 0 {
		t.Errorf("icon-192 corner alpha = %d, want transparent", a)
	}
	if r, _, _, a := icon.At(150, 150).RGBA(); r != 0xffff || a != 0xffff {
		t.Errorf("icon-192 body = %d/%d, want opaque red", r, a)
	}
}

func TestNoManifestNoThemeColor(t *testing.T) {
	app := site(t, &collage.Config{}, favicon.New(favicon.Options{Image: source(t, 512, 512), NoManifest: true}))
	if rec := get(app, "/site.webmanifest"); rec.Code != http.StatusNotFound {
		t.Errorf("/site.webmanifest = %d", rec.Code)
	}
	page := get(app, "/").Body.String()
	if strings.Contains(page, "manifest") || strings.Contains(page, "theme-color") {
		t.Errorf("head = %s", page)
	}
}

// The source is read from FS when one is given, and from disk through the
// application's JSON configuration otherwise.
func TestSourcePaths(t *testing.T) {
	fsys := fstest.MapFS{"icons/icon.png": {Data: source(t, 512, 512)}, "icons/icon.svg": {Data: []byte(svg)}}
	app := site(t, &collage.Config{}, favicon.New(favicon.Options{FS: fsys, Source: "icons/icon.png", SVG: "icons/icon.svg"}))
	if rec := get(app, "/icon.svg"); rec.Code != http.StatusOK {
		t.Errorf("from FS: /icon.svg = %d", rec.Code)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "icon.png")
	if err := os.WriteFile(path, source(t, 512, 512), 0o600); err != nil {
		t.Fatal(err)
	}
	conf, _ := json.Marshal(map[string]string{"source": path, "name": "Configured"})
	app = site(t, &collage.Config{PluginConfig: map[string]json.RawMessage{favicon.Name: conf}}, favicon.New(favicon.Options{}))
	if rec := get(app, "/site.webmanifest"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"Configured"`) {
		t.Errorf("from configuration: %d %s", rec.Code, rec.Body.String())
	}
}

// A source the plugin cannot use stops the application: the handler answers 503.
func TestRefusals(t *testing.T) {
	for name, opts := range map[string]favicon.Options{
		"no source":     {},
		"missing file":  {Source: filepath.Join(t.TempDir(), "nope.png")},
		"undecodable":   {Image: []byte("not an image")},
		"not square":    {Image: source(t, 512, 300)},
		"bad svg":       {Image: source(t, 512, 512), SVGImage: []byte("<html></html>")},
		"bad colour":    {Image: source(t, 512, 512), ThemeColor: `red" onload="x`},
		"bad bg colour": {Image: source(t, 512, 512), BackgroundColor: "#12345"},
	} {
		t.Run(name, func(t *testing.T) {
			app := site(t, &collage.Config{Logger: slog.New(slog.DiscardHandler)}, favicon.New(opts))
			if rec := get(app, "/favicon.ico"); rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503", rec.Code)
			}
		})
	}
}

// Nearly square is accepted and centred; small is accepted with a warning.
func TestNearlySquareAndSmall(t *testing.T) {
	var logs bytes.Buffer
	app := site(t, &collage.Config{Logger: slog.New(slog.NewTextHandler(&logs, nil))},
		favicon.New(favicon.Options{Image: source(t, 200, 190)}))
	rec := get(app, "/icon-512.png")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if b := decodePNG(t, rec.Body.Bytes()).Bounds(); b.Dx() != 512 || b.Dy() != 512 {
		t.Errorf("icon-512 = %v", b)
	}
	if !strings.Contains(logs.String(), "smaller than 512") {
		t.Errorf("no warning logged: %s", logs.String())
	}
}

// A static build writes every icon at the root of its output.
func TestStaticBuild(t *testing.T) {
	app := site(t, &collage.Config{}, favicon.New(favicon.Options{Image: source(t, 512, 512), SVGImage: []byte(svg)}))
	out := t.TempDir()
	b, err := collage.NewBuilder(app, collage.BuildOptions{OutDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"favicon.ico", "apple-touch-icon.png", "icon-192.png", "icon-512.png", "icon.svg", "site.webmanifest"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("build lacks %s: %v", name, err)
		}
	}
	index, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(index, []byte(`rel="apple-touch-icon"`)) {
		t.Errorf("built page lacks the icons: %s", index)
	}
}
