package src

//go:generate sh -c "cd ../ts && sh compileJS.sh"

import "embed"

//go:embed all:html
var webUI embed.FS
