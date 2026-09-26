# elagoht/favicon

A collage plugin that makes a site's icons from one source image — favicon.ico,
the Apple touch icon, the web app icons and the manifest that lists them — serves
them at the root, and links them from every page's head. Registering it is the
whole of it: nothing in a page has to ask.

```go
app, err := collage.New(&collage.Config{
	Plugins: []collage.Plugin{favicon.New(favicon.Options{
		Source:          "assets/icon.png",
		SVG:             "assets/icon.svg",
		Name:            "The blog",
		ThemeColor:      "#0f172a",
		BackgroundColor: "#ffffff",
	})},
})
```

Requires collage v0.23.0 or later.

## What is made

From one square image, ideally 512 pixels or larger:

| File | What it is |
| --- | --- |
| `/favicon.ico` | 16, 32 and 48 pixels, each entry a PNG |
| `/apple-touch-icon.png` | 180 pixels, for an iOS home screen |
| `/icon-192.png`, `/icon-512.png` | the web app icons the manifest lists |
| `/icon.svg` | the SVG you gave, unchanged — only when you gave one |
| `/site.webmanifest` | name, short name, start URL, display mode, colours and icons |

Everything is resized with Catmull-Rom from `golang.org/x/image/draw`, made once
when the application starts, and served as static documents at the root: cached,
and written by a static build like any other file. An SVG is passed through, never
rasterised, so it cannot stand in for the raster source.

The Apple touch icon's transparent pixels are filled with `BackgroundColor` when it
is a hex colour: iOS paints them black otherwise. The other icons keep their
transparency.

## In the head

Every page is given, through `{{hoist "head"}}`:

```html
<link rel="icon" href="/favicon.ico?v=3f9a1c02" sizes="32x32">
<link rel="icon" href="/icon.svg?v=3f9a1c02" type="image/svg+xml">
<link rel="apple-touch-icon" href="/apple-touch-icon.png?v=3f9a1c02">
<link rel="manifest" href="/site.webmanifest?v=3f9a1c02">
<meta name="theme-color" content="#0f172a">
```

The SVG link only when there is an SVG, the manifest unless `NoManifest`, the theme
colour only when `ThemeColor` is set. `?v=` is a digest of the source: browsers keep
a favicon far longer than any `Cache-Control` asks, and a changed URL is the one
thing they do not argue with. The documents ignore the query, so `/favicon.ico`
itself — which some clients request without looking at the page — is always there.

Each tag is hoisted under a key (`favicon:ico`, `favicon:svg`, `favicon:apple`,
`favicon:manifest`, `favicon:theme-color`), so a page can replace one by hoisting
its own under the same key. The layout must place `{{hoist "head"}}`; without it,
nothing appears.

## Where the source comes from

- `Source` and `SVG` are paths. They are read from disk, relative to the working
  directory — or from `FS` when it is set, an `embed.FS` so the binary runs from
  anywhere.
- `Image` and `SVGImage` are the files' bytes, set in Go, and take the place of
  the paths.

The source may be a PNG, a JPEG or a GIF.

## Refused at startup

The application does not start — the handler answers 503 and logs why — when:

- there is no source, or it cannot be read or decoded;
- its sides differ by more than a tenth (a nearly square image is centred on a
  transparent square, not stretched);
- the SVG holds no `<svg>` element;
- `ThemeColor` or `BackgroundColor` is neither a hex colour nor a colour name.

A source smaller than 512 pixels is accepted with a warning in the log: the larger
icons are upscaled from it, and look it.

## Options

| Option | JSON | Default | |
| --- | --- | --- | --- |
| `Source` | `source` | — | path of the source image |
| `SVG` | `svg` | — | path of an SVG version, served as `/icon.svg` |
| `FS` | — | disk | where `Source` and `SVG` are read from |
| `Image`, `SVGImage` | — | — | the files' bytes, in place of the paths |
| `Name` | `name` | — | the manifest's `name` |
| `ShortName` | `shortName` | `Name` | the manifest's `short_name` |
| `ThemeColor` | `themeColor` | — | `theme_color` and `<meta name="theme-color">` |
| `BackgroundColor` | `backgroundColor` | — | `background_color`, and the Apple touch icon's fill |
| `Display` | `display` | `standalone` | the manifest's display mode |
| `StartURL` | `startUrl` | `/` | the page an installed app opens on |
| `NoManifest` | `noManifest` | `false` | serve no manifest, and link none |

## Configuration

```json
{
  "elagoht/favicon": {
    "source": "assets/icon.png",
    "svg": "assets/icon.svg",
    "name": "The blog",
    "shortName": "Blog",
    "themeColor": "#0f172a",
    "backgroundColor": "#ffffff"
  }
}
```

## Limitations

- **No SVG rasterisation.** An SVG is served as it is; every raster icon comes from
  the raster source. Rasterising SVG in Go means a dependency larger than this
  plugin.
- **The paths are fixed at the root.** `/favicon.ico` is where browsers look without
  being told, and the rest follow it; there is no option to move them.
- **No dark-mode or maskable variants.** One source, one set of icons. A maskable
  icon needs its own artwork with a safe zone, not a resize.
- **Colour names are not parsed** for the Apple touch icon's fill: with a name
  rather than a hex colour it keeps its transparency, and iOS paints it black.
- **The source is read once, at startup.** Changing it needs a restart, in
  development too.
- **An ICO of PNG entries** is read by every browser since Internet Explorer 9, not
  by older Windows shell components that expect bitmaps.
