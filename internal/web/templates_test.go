package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

var (
	// scriptTagRe finds every <script ...> opening tag.
	scriptTagRe = regexp.MustCompile(`(?i)<script\b[^>]*>`)
	// inlineHandlerRe finds on*= attributes inside a tag.
	inlineHandlerRe = regexp.MustCompile(`(?i)<[a-z][^>]*\son[a-z]+\s*=`)
	// styleAttrRe finds style= attributes inside a tag.
	styleAttrRe = regexp.MustCompile(`(?i)<[a-z][^>]*\sstyle\s*=`)
	// rawHTMLRe finds any use of the unsafe-HTML escape hatch.
	rawHTMLRe = regexp.MustCompile(`template\.HTML|template\.JS|template\.URL|template\.CSS`)
)

// TestTemplatesHaveNoInlineScriptOrStyle: with no 'unsafe-inline' in the CSP,
// any inline script, handler attribute or style attribute would be blocked,
// so the templates must contain none, and the only script is the pinned htmx.
func TestTemplatesHaveNoInlineScriptOrStyle(t *testing.T) {
	entries, err := fs.ReadDir(templateFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 12 {
		t.Fatalf("found %d templates; expected layout, matches, near_misses, pair, history, route, route_result, decision, log, eval, error, admin", len(entries))
	}
	for _, e := range entries {
		raw, err := fs.ReadFile(templateFS, "templates/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		for _, tag := range scriptTagRe.FindAllString(src, -1) {
			if !strings.Contains(tag, `src="{{.Assets.HtmxURL}}"`) || !strings.Contains(tag, `integrity="{{.Assets.HtmxSRI}}"`) || !strings.Contains(tag, `crossorigin="anonymous"`) {
				t.Errorf("%s: script tag is not the pinned htmx with SRI: %s", e.Name(), tag)
			}
		}
		if m := inlineHandlerRe.FindString(src); m != "" {
			t.Errorf("%s: inline event handler: %s", e.Name(), m)
		}
		if m := styleAttrRe.FindString(src); m != "" {
			t.Errorf("%s: inline style attribute: %s", e.Name(), m)
		}
		if strings.Contains(src, "javascript:") {
			t.Errorf("%s: javascript: URL", e.Name())
		}
	}
	for _, f := range []string{"public.go", "handlers_matches.go", "handlers_pairs.go", "handlers_route.go", "middleware_csp.go",
		"handlers_nearmisses.go", "handlers_log.go", "handlers_eval.go", "history.go", "collector_stats.go", "admin.go"} {
		src, err := fs.ReadFile(sourceFS(t), f)
		if err != nil {
			t.Fatal(err)
		}
		if m := rawHTMLRe.FindString(string(src)); m != "" {
			t.Errorf("%s uses %s; external data must only go through auto-escaping (AB-07)", f, m)
		}
	}
}

// TestLayoutPinsCDNAssetsWithSRI renders a page and checks both CDN assets
// are version-pinned, carry sha384 integrity hashes and crossorigin, and that
// the CSP's CDN host matches the asset URLs.
func TestLayoutPinsCDNAssetsWithSRI(t *testing.T) {
	rec := get(New(Config{}, Deps{}).Handler(), "/nowhere-pairs")
	if rec.Code != 404 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := get(New(Config{}, Deps{}).Handler(), "/").Body.String()
	wantContains(t, body,
		`<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@picocss/pico@2.1.1/css/pico.classless.min.css" integrity="sha384-`,
		`<script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.11/dist/htmx.min.js" integrity="sha384-`,
		`crossorigin="anonymous"`,
		`<meta name="htmx-config"`, `"allowEval":false`, `"includeIndicatorStyles":false`, `"selfRequestsOnly":true`,
		`<link rel="stylesheet" href="/static/app.css">`,
	)
	for _, u := range []string{picoURL, htmxURL} {
		if !strings.HasPrefix(u, cdnHost+"/") {
			t.Errorf("asset %s is not on the CSP host %s", u, cdnHost)
		}
	}
	for _, sri := range []string{picoSRI, htmxSRI} {
		if !strings.HasPrefix(sri, "sha384-") || len(sri) != len("sha384-")+64 {
			t.Errorf("SRI %q is not a base64 sha384", sri)
		}
	}
	if strings.Count(body, "<script") != 1 {
		t.Errorf("page has %d script tags, want exactly the htmx one", strings.Count(body, "<script"))
	}
}
