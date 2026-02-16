package web

import (
	"embed"
	"html/template"
)

//go:embed templates/index.html
var templateFiles embed.FS

var indexTemplate = template.Must(template.ParseFS(templateFiles, "templates/index.html"))
