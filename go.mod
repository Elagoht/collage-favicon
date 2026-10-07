// A collage plugin that makes a site's icons from one source image: favicon.ico,
// the Apple touch icon, the web app icons and the manifest that lists them, served
// at the root and linked from every page's head.
module github.com/Elagoht/collage-favicon

go 1.26.0

require github.com/Elagoht/collage v0.50.0

require golang.org/x/image v0.46.0

retract v0.1.3 // tagged by mistake on the previous release's code; use v0.1.4 or later
