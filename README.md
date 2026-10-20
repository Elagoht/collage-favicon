# elagoht/favicon

A collage plugin that makes a site's icons from one source image — favicon.ico,
the Apple touch icon, the web app icons and the manifest that lists them — serves
them at the root, and links them from every page's head. Registering it is the
whole of it: nothing in a page has to ask.

```sh
collage plugin add elagoht/favicon
```

That is all it needs. The icons are made from the site's own `static/icon.png`
(and `static/icon.svg`, if there is one), and until there is one they are
collage's. Put a square PNG, 512 pixels or larger, there and restart.

```json
{
  "elagoht/favicon": {
    "name": "The blog",
    "themeColor": "#0f172a",
    "backgroundColor": "#ffffff"
  }
}
```

Requires collage v0.68.0 or later.

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

- By default, `/static/icon.png` and `/static/icon.svg`: URLs of the site's own,
  read through the application's mount that serves them (`Host.OpenMounted`).
  That is the disk in development and the copy the binary embeds once built, so
  nothing has to be copied beside the binary. The SVG is optional.
- `Source` and `SVG` name another file. One starting with `/` is a URL, read the
  same way; any other path is read from disk, relative to the working directory,
  or from `FS` when it is set.
- `SourceBytes` and `SVGBytes` are the files' bytes, set in Go, and take the place of
  the paths.

The source may be a PNG, a JPEG or a GIF.

## When the source cannot be used

The application starts anyway, with collage's icons, and the log says why:

- no source at the default URL is an info line saying where to put one;
- a source that was named but is missing or cannot be decoded, or whose sides
  differ by more than a tenth, is a warning (a nearly square image is centred on
  a transparent square, not stretched);
- an SVG that holds no `<svg>` element is left out, with a warning.

A source smaller than 512 pixels is accepted with a warning: the larger icons are
upscaled from it, and look it. Only a `ThemeColor` or `BackgroundColor` that is
neither a hex colour nor a colour name stops the application: that is a mistake
in the configuration, not a missing file.

## Options

| Option | JSON | Default | |
| --- | --- | --- | --- |
| `Source` | `source` | `/static/icon.png` | the source image: a URL of the site's, or a path |
| `SVG` | `svg` | `/static/icon.svg`, if there is one | an SVG version, served as `/icon.svg` |
| `FS` | — | disk | where a `Source` or `SVG` that is a path is read from |
| `SourceBytes`, `SVGBytes` | — | — | the files' bytes, in place of the paths |
| `Name` | `name` | — | the manifest's `name` |
| `ShortName` | `shortName` | `Name` | the manifest's `short_name` |
| `ThemeColor` | `themeColor` | — | `theme_color` and `<meta name="theme-color">` |
| `BackgroundColor` | `backgroundColor` | — | `background_color`, and the Apple touch icon's fill |
| `Display` | `display` | `standalone` | the manifest's display mode |
| `StartURL` | `startURL` | `/` | the page an installed app opens on |
| `NoManifest` | `noManifest` | `false` | serve no manifest, and link none |

## Configuration

```json
{
  "elagoht/favicon": {
    "source": "/static/brand/icon.png",
    "svg": "/static/brand/icon.svg",
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

## Changes

### v0.3.0

- **No configuration needed.** The source defaults to the site's own
  `/static/icon.png` (and `/static/icon.svg`), read through the application's
  mount, so a built binary carries it.
- **A source that cannot be used no longer stops the application.** It starts
  with collage's icons and logs why; `ErrNoSource` is no longer returned.
- A `source` starting with `/` is a URL of the site's. A path without one is read
  as before.
- Requires collage v0.68.0 (`Host.OpenMounted`).

### v0.2.0

Changed in v0.2.0, for the v1 plugin conventions:

| was | now |
|---|---|
| `Options.Image` | `Options.SourceBytes` |
| `Options.SVGImage` | `Options.SVGBytes` |
| `"startUrl"` | `"startURL"` (the old spelling still decodes: keys match case-insensitively) |

- Requires collage v0.61.0.
