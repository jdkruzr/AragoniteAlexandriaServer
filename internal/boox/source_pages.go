package boox

import "html/template"

var booxSettings = template.Must(template.New("settings.html").Funcs(viewFuncs()).ParseFS(templateFS, "templates/settings.html"))
