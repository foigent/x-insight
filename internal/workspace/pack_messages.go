package workspace

import (
	"embed"
	"encoding/json"
	"html/template"
)

//go:embed pack.en-US.json pack.zh-CN.json
var packMessageFiles embed.FS

var evidencePackTemplates = func() map[string]*template.Template {
	result := make(map[string]*template.Template, 2)
	var expected int
	for _, language := range []string{"en-US", "zh-CN"} {
		raw, err := packMessageFiles.ReadFile("pack." + language + ".json")
		if err != nil {
			panic(err)
		}
		messages := map[string]string{}
		if err := json.Unmarshal(raw, &messages); err != nil {
			panic(err)
		}
		if expected != 0 && len(messages) != expected {
			panic("evidence pack message catalogs differ")
		}
		expected = len(messages)
		result[language] = template.Must(template.New("evidence-pack").Funcs(template.FuncMap{
			"tr": func(key string) string {
				value, ok := messages[key]
				if !ok || value == "" {
					panic("missing evidence pack message: " + key)
				}
				return value
			},
		}).Parse(evidencePackTemplateSource))
	}
	return result
}()
