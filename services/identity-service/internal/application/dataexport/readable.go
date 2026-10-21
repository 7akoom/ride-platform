package dataexport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"strconv"
	"strings"
	"time"
)

// ReadableName is the page in the ZIP that a person opens in a browser: every
// section as a table, in the language they asked in.
const ReadableName = "index.html"

// field is one key of a JSON object, in the order the file has it.
type field struct {
	Key   string
	Value any
}

type table struct {
	Title   string
	Columns []string
	Rows    [][]string
	Pairs   bool
}

type page struct {
	Lang, Dir, Title, Intro, Contents, Times, Empty string
	Tables                                          []table
}

// BuildReadable writes index.html from the sections. Times are shown in zone, and
// every number in Western digits.
func BuildReadable(export Export, sections []Section, zone *time.Location) ([]byte, error) {
	if zone == nil {
		zone = time.UTC
	}

	locale := export.Locale
	words, ok := pageText[locale]
	if !ok {
		locale, words = "en", pageText["en"]
	}

	lang := languageIndex(locale)
	render := valueRenderer{zone: zone, yes: words.yes, no: words.no, lang: lang}

	p := page{
		Lang: locale, Dir: words.dir, Title: words.title, Contents: words.files, Empty: words.empty,
		Intro: fmt.Sprintf(words.intro, isolate(export.RequestedAt.In(zone).Format("2006-01-02 15:04"))),
		Times: fmt.Sprintf(words.times, isolate(zone.String())),
	}

	for _, section := range sections {
		t, err := sectionTable(section, lang, render)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", section.Name, err)
		}

		p.Tables = append(p.Tables, t)
	}

	var out bytes.Buffer
	if err := pageTemplate.Execute(&out, p); err != nil {
		return nil, fmt.Errorf("render %s: %w", ReadableName, err)
	}

	return out.Bytes(), nil
}

func sectionTable(section Section, lang int, render valueRenderer) (table, error) {
	t := table{Title: section.Name}
	if titles, ok := sectionTitles[section.Name]; ok {
		t.Title = titles[lang]
	}

	value, err := decodeOrdered(section.Content)
	if err != nil {
		// Not JSON: shown as it is, so one odd section never costs the export.
		t.Rows = [][]string{{string(section.Content)}}

		return t, nil
	}

	switch v := value.(type) {
	case []field:
		t.Pairs = true
		for _, f := range v {
			t.Rows = append(t.Rows, []string{fieldLabel(f.Key, lang), render.field(f.Key, f.Value)})
		}

	case []any:
		var keys []string
		seen := map[string]bool{}

		for _, item := range v {
			object, ok := item.([]field)
			if !ok {
				t.Rows = append(t.Rows, []string{render.text(item)})

				continue
			}

			for _, f := range object {
				if !seen[f.Key] {
					seen[f.Key] = true
					keys = append(keys, f.Key)
				}
			}
		}

		for _, key := range keys {
			t.Columns = append(t.Columns, fieldLabel(key, lang))
		}

		for _, item := range v {
			object, ok := item.([]field)
			if !ok {
				continue
			}

			byKey := map[string]any{}
			for _, f := range object {
				byKey[f.Key] = f.Value
			}

			row := make([]string, len(keys))
			for i, key := range keys {
				row[i] = render.field(key, byKey[key])
			}

			t.Rows = append(t.Rows, row)
		}

	case nil:
	default:
		t.Rows = [][]string{{render.text(v)}}
	}

	return t, nil
}

// isolate keeps a left-to-right value (a date, a zone) in one piece inside
// right-to-left text (Unicode isolates).
func isolate(value string) string { return "\u2066" + value + "\u2069" }

func fieldLabel(key string, lang int) string {
	if labels, ok := fieldLabels[key]; ok {
		return labels[lang]
	}

	return strings.ReplaceAll(key, "_", " ")
}

type valueRenderer struct {
	zone    *time.Location
	yes, no string
	lang    int
}

// field shows a value of the field key, a system word in the reader's language.
func (r valueRenderer) field(key string, value any) string {
	if word, ok := value.(string); ok && enumFields[key] {
		if labels, ok := valueLabels[word]; ok {
			return labels[r.lang]
		}
	}

	return r.text(value)
}

// text shows one value: a time in the zone, a boolean as yes or no, anything
// nested as compact JSON, nothing as a dash.
func (r valueRenderer) text(value any) string {
	switch v := value.(type) {
	case nil:
		return "—"
	case bool:
		if v {
			return r.yes
		}

		return r.no
	case json.Number:
		return v.String()
	case []any:
		if len(v) == 0 {
			return "—"
		}

		return compactJSON(v)
	case string:
		if at, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return at.In(r.zone).Format("2006-01-02 15:04")
		}

		return v
	default:
		return compactJSON(v)
	}
}

func compactJSON(value any) string {
	var b strings.Builder

	writeJSON(&b, value)

	return b.String()
}

func writeJSON(b *strings.Builder, value any) {
	switch v := value.(type) {
	case []field:
		b.WriteByte('{')
		for i, f := range v {
			if i > 0 {
				b.WriteString(", ")
			}

			b.WriteString(strconv.Quote(f.Key) + ": ")
			writeJSON(b, f.Value)
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteString(", ")
			}

			writeJSON(b, item)
		}
		b.WriteByte(']')
	case string:
		b.WriteString(strconv.Quote(v))
	case nil:
		b.WriteString("null")
	default:
		fmt.Fprint(b, v)
	}
}

// decodeOrdered reads JSON keeping the order of each object's keys: objects come
// back as []field, arrays as []any, numbers as json.Number.
func decodeOrdered(content []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()

	value, err := decodeValue(decoder)
	if err != nil {
		return nil, err
	}

	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("more than one JSON value")
	}

	return value, nil
}

func decodeValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}

	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			object := []field{}

			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}

				value, err := decodeValue(decoder)
				if err != nil {
					return nil, err
				}

				object = append(object, field{Key: keyToken.(string), Value: value})
			}

			_, err := decoder.Token()

			return object, err
		case '[':
			list := []any{}

			for decoder.More() {
				value, err := decodeValue(decoder)
				if err != nil {
					return nil, err
				}

				list = append(list, value)
			}

			_, err := decoder.Token()

			return list, err
		}

		return nil, fmt.Errorf("unexpected %v", t)
	default:
		return token, nil
	}
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="{{.Lang}}" dir="{{.Dir}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
body { font-family: system-ui, "Segoe UI", Tahoma, sans-serif; margin: 0; padding: 16px; color: #05070F; background: #fff; }
h1 { font-size: 24px; margin: 0 0 8px; }
h2 { font-size: 18px; margin: 32px 0 8px; padding-bottom: 4px; border-bottom: 2px solid #F3D59A; }
p { color: #444; max-width: 70ch; }
nav ul { padding-inline-start: 20px; }
.scroll { overflow-x: auto; }
table { border-collapse: collapse; font-size: 14px; }
th, td { border: 1px solid #ddd; padding: 6px 10px; text-align: start; vertical-align: top; }
th { background: #f6f6f6; white-space: nowrap; }
td { white-space: nowrap; }
td.long { white-space: normal; min-width: 24ch; max-width: 48ch; overflow-wrap: break-word; }
.empty { color: #888; }
</style>
</head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Intro}}</p>
<p>{{.Times}}</p>
<nav><h2>{{.Contents}}</h2><ul>{{range $i, $t := .Tables}}<li><a href="#s{{$i}}">{{$t.Title}}</a></li>{{end}}</ul></nav>
{{range $i, $t := .Tables}}
<h2 id="s{{$i}}">{{$t.Title}}</h2>
{{if not $t.Rows}}<p class="empty">{{$.Empty}}</p>{{else}}<div class="scroll"><table>
{{if $t.Columns}}<tr>{{range $t.Columns}}<th>{{.}}</th>{{end}}</tr>{{end}}
{{range $t.Rows}}<tr>{{range $j, $cell := .}}{{if and $t.Pairs (eq $j 0)}}<th>{{$cell}}</th>{{else}}<td{{if gt (len $cell) 40}} class="long"{{end}}><bdi>{{$cell}}</bdi></td>{{end}}{{end}}</tr>
{{end}}</table></div>{{end}}
{{end}}
</body>
</html>
`))
