package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
)

var page = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html>
<head><title>relay-primitive</title><style>
body { font-family: monospace; padding: 2em; }
h2 { border-bottom: 1px solid #ccc; }
table { border-collapse: collapse; width: 100%; }
td { padding: 2px 8px; vertical-align: top; }
td:first-child { white-space: nowrap; font-weight: bold; color: #555; width: 30%; }
</style></head>
<body>
<h1>relay-primitive</h1>

<h2>Request</h2>
<table>
<tr><td>Method</td><td>{{.Method}}</td></tr>
<tr><td>Path</td><td>{{.Path}}</td></tr>
<tr><td>Raw query</td><td>{{.RawQuery}}</td></tr>
</table>

<h2>Query Parameters</h2>
<table>
{{- range .QueryParams}}
<tr><td>{{.Key}}</td><td>{{range .Values}}{{.}}<br>{{end}}</td></tr>
{{- else}}<tr><td colspan="2">(none)</td></tr>{{end}}
</table>

<h2>Headers</h2>
<table>
{{- range .Headers}}
<tr><td>{{.Key}}</td><td>{{range .Values}}{{.}}<br>{{end}}</td></tr>
{{- end}}
</table>

<h2>Environment</h2>
<table>
{{- range .Env}}
<tr><td>{{.Key}}</td><td>{{.Val}}</td></tr>
{{- end}}
</table>
</body></html>
`))

type kv struct{ Key, Val string }
type kvmulti struct {
	Key    string
	Values []string
}

func handler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	queryKeys := make([]string, 0, len(q))
	for k := range q {
		queryKeys = append(queryKeys, k)
	}
	sort.Strings(queryKeys)
	queryParams := make([]kvmulti, 0, len(queryKeys))
	for _, k := range queryKeys {
		queryParams = append(queryParams, kvmulti{Key: k, Values: q[k]})
	}

	headerKeys := make([]string, 0, len(r.Header))
	for k := range r.Header {
		headerKeys = append(headerKeys, k)
	}
	sort.Strings(headerKeys)
	headers := make([]kvmulti, 0, len(headerKeys))
	for _, k := range headerKeys {
		headers = append(headers, kvmulti{Key: k, Values: r.Header[k]})
	}

	rawEnv := os.Environ()
	sort.Strings(rawEnv)
	env := make([]kv, 0, len(rawEnv))
	for _, e := range rawEnv {
		k, v, _ := strings.Cut(e, "=")
		env = append(env, kv{Key: k, Val: v})
	}

	data := struct {
		Method      string
		Path        string
		RawQuery    string
		QueryParams []kvmulti
		Headers     []kvmulti
		Env         []kv
	}{
		Method:      r.Method,
		Path:        r.URL.Path,
		RawQuery:    r.URL.RawQuery,
		QueryParams: queryParams,
		Headers:     headers,
		Env:         env,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := page.Execute(w, data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/", handler)
	log.Printf("relay-primitive listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
