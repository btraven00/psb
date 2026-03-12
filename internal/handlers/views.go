package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"

	"github.com/btraven00/psb/internal/models"
	"github.com/labstack/echo/v4"
)

// CommitHash is set at build time via ldflags, with runtime VCS fallback.
var CommitHash = func() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				return s.Value
			}
		}
	}
	return "dev"
}()

const pageSize = 40

// safeID matches alphanumeric, underscores, hyphens only (no path traversal, no injection).
var safeID = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

// --- shared CSS (injected into all pages) ---
const sharedCSS = `
  :root, [data-theme="dark"] {
    --bg: #0a0a0a;
    --fg: #c0c0c0;
    --dim: #555;
    --accent: #7aa2f7;
    --green: #9ece6a;
    --red: #f7768e;
    --yellow: #e0af68;
    --border: #1a1a2e;
    --row-alt: #0f0f17;
    --hover: #16161e;
    --td-border: #111;
  }
  [data-theme="light"] {
    --bg: #f5f5f5;
    --fg: #1a1a1a;
    --dim: #888;
    --accent: #2563eb;
    --green: #16a34a;
    --red: #dc2626;
    --yellow: #ca8a04;
    --border: #d4d4d4;
    --row-alt: #ebebeb;
    --hover: #e0e0e0;
    --td-border: #ddd;
  }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: "JetBrains Mono", "Fira Code", "SF Mono", "Cascadia Code", monospace;
    font-size: 13px;
    background: var(--bg);
    color: var(--fg);
    padding: 1rem 3rem 2.5rem 3rem;
    line-height: 1.5;
  }
  a { color: var(--accent); text-decoration: none; }
  a:hover { text-decoration: underline; }
  .header {
    border-bottom: 1px solid var(--border);
    padding-bottom: 0.5rem;
    margin-bottom: 1rem;
  }
  .header h1 {
    font-size: 14px;
    font-weight: 400;
    color: var(--dim);
    letter-spacing: 0.1em;
    text-transform: lowercase;
  }
  .header h1 span { color: var(--accent); }
  .nav { font-size: 11px; margin-top: 0.5rem; }
  .nav a { color: var(--dim); }
  .nav a:hover { color: var(--accent); }
  .section-label {
    font-size: 11px;
    color: var(--dim);
    text-transform: uppercase;
    letter-spacing: 0.15em;
    margin: 1rem 0 0.3rem 0;
  }
  .section-label::before {
    content: "// ";
    color: var(--border);
  }
  table {
    border-collapse: collapse;
    width: 100%;
    margin-bottom: 0.5rem;
  }
  th {
    font-size: 11px;
    font-weight: 400;
    color: var(--dim);
    text-transform: lowercase;
    letter-spacing: 0.05em;
    text-align: left;
    padding: 4px 8px;
    border-bottom: 1px solid var(--border);
  }
  td {
    padding: 3px 8px;
    border-bottom: 1px solid var(--td-border);
    white-space: nowrap;
    font-size: 12px;
  }
  tr:nth-child(even) td { background: var(--row-alt); }
  tr:hover td { background: var(--hover); }
  .id { color: var(--dim); }
  .tool { color: var(--accent); font-weight: 600; }
  .cmd { color: var(--fg); }
  .param { color: var(--dim); font-style: italic; }
  .size { color: var(--yellow); }
  .time { color: var(--green); }
  .exit-ok { color: var(--dim); }
  .exit-fail { color: var(--red); font-weight: 600; }
  .ts { color: var(--dim); font-size: 11px; }
  .session { color: var(--yellow); font-size: 11px; }
  .record { color: var(--dim); font-size: 11px; }
  .page-footer {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 6px 10px;
    background: var(--bg);
    border-top: 1px solid var(--border);
    z-index: 10;
  }
  .page-footer-left {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .pagination {
    display: flex;
    gap: 2px;
  }
  .pagination a, .pagination .current {
    display: inline-block;
    padding: 2px 10px;
    text-decoration: none;
    color: var(--dim);
    border: 1px solid var(--border);
    font-size: 12px;
  }
  .pagination a:hover { color: var(--fg); border-color: var(--dim); }
  .pagination .ellipsis { color: var(--dim); font-size: 12px; padding: 2px 4px; }
  .pagination .current {
    color: var(--accent);
    border-color: var(--accent);
  }
  .meta {
    color: var(--dim);
    font-size: 11px;
  }
  .empty { color: var(--dim); padding: 1rem 0; }
  .layout { display: flex; gap: 2rem; }
  .sidebar {
    flex: 0 0 280px;
    position: sticky;
    top: 2rem;
    align-self: flex-start;
    max-height: calc(100vh - 4rem);
    overflow-y: auto;
  }
  .main { flex: 1; min-width: 0; overflow-x: auto; }
  .stat-list { list-style: none; margin: 0; padding: 0; }
  .stat-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 4px 0;
    border-bottom: 1px solid var(--border);
    font-size: 11px;
  }
  .stat-item a { color: var(--fg); }
  .stat-item a:hover { color: var(--accent); }
  .stat-item .count {
    color: var(--dim);
    font-size: 10px;
    background: var(--row-alt);
    padding: 1px 6px;
    border-radius: 3px;
  }
  .stat-item.active a { color: var(--accent); font-weight: 600; }
  .field { margin: 0.3rem 0; }
  .field .label { color: var(--dim); display: inline-block; min-width: 18ch; }
  .field .val { color: var(--fg); }
  .field .val.accent { color: var(--accent); }
  .field .val.green { color: var(--green); }
  .field .val.yellow { color: var(--yellow); }
  .field .val.red { color: var(--red); }
  .theme-toggle {
    background: none;
    border: 1px solid var(--border);
    color: var(--dim);
    font-family: inherit;
    font-size: 11px;
    padding: 2px 8px;
    cursor: pointer;
    letter-spacing: 0.05em;
  }
  .theme-toggle:hover { color: var(--fg); border-color: var(--dim); }
  .build-footer { font-size: 10px; color: var(--dim); font-family: monospace; letter-spacing: 0.05em; }
  .header-row { display: flex; justify-content: space-between; align-items: center; }
`

const themeJS = `
<script>
(function(){
  var t = localStorage.getItem('theme') || 'dark';
  document.documentElement.setAttribute('data-theme', t);
  window.toggleTheme = function(){
    t = t === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', t);
    localStorage.setItem('theme', t);
  };
})();
</script>`

// ============================================================
// Main listing view
// ============================================================
const viewTmpl = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>psb // telemetry</title>
<style>` + sharedCSS + `</style>
` + themeJS + `
</head>
<body>

<div class="header">
  <div class="header-row">
    <h1><span>psb</span> // planetary scale benchmark</h1>
    <button class="theme-toggle" onclick="toggleTheme()">theme</button>
  </div>
</div>

<div class="layout">

<div class="sidebar">
  <div class="section-label">top platforms</div>
  <ul class="stat-list">
    {{range .TopPlatforms}}
    <li class="stat-item{{if eq .Name $.PlatformFilter}} active{{end}}">
      <a href="{{.URL}}">{{.Name}}</a>
      <span class="count">{{.Count}}</span>
    </li>
    {{else}}
    <li class="stat-item"><span class="empty">--</span></li>
    {{end}}
  </ul>
  {{if .PlatformFilter}}<div style="margin-top:0.3rem;font-size:10px;"><a href="{{.ClearPlatformURL}}" style="color:var(--red);">[clear platform]</a></div>{{end}}

  <div class="section-label">top snakemake</div>
  <ul class="stat-list">
    {{range .TopSnakemake}}
    <li class="stat-item{{if eq .Name $.SmFilter}} active{{end}}">
      <a href="{{.URL}}">{{.Name}}</a>
      <span class="count">{{.Count}}</span>
    </li>
    {{else}}
    <li class="stat-item"><span class="empty">--</span></li>
    {{end}}
  </ul>
  {{if .SmFilter}}<div style="margin-top:0.3rem;font-size:10px;"><a href="{{.ClearSmURL}}" style="color:var(--red);">[clear snakemake]</a></div>{{end}}
</div>

<div class="main">
  <div class="section-label">execution metrics{{if .ToolFilter}} <span style="color:var(--accent);font-size:11px;text-transform:none;letter-spacing:0;">— tool: {{.ToolFilter}} <a href="{{.ClearToolURL}}" style="color:var(--red);font-size:10px;">[clear]</a></span>{{end}}</div>
  <table>
    <tr>
      <th>session</th><th>record</th><th>platform</th><th>tool</th><th>command</th>
      <th>input</th><th>type</th><th>runtime</th><th>rss_mb</th>
      <th>cpu</th><th>timestamp</th>
    </tr>
    {{range .Metrics}}
    <tr>
      <td class="session"><a href="/session/{{.SessionID}}">{{.SessionID}}</a></td>
      <td class="record"><a href="/record/{{.RecordID}}">{{.RecordID}}</a></td>
      <td class="id"><a href="/?platform={{index $.EnvMap .EnvironmentID}}">{{index $.EnvMap .EnvironmentID}}</a></td>
      <td class="tool"><a href="/?tool={{.Tool}}">{{.Tool}}</a></td>
      <td class="cmd">{{.CommandPattern}}</td>
      <td class="size">{{.InputSizeHuman}}</td>
      <td>{{.InputTypeClean}}</td>
      <td class="time">{{printf "%.2f" .RuntimeSec}}s</td>
      <td>{{printf "%.1f" .MaxRSS}}</td>
      <td>{{printf "%.1f" .AvgCPUPercent}}</td>
      <td class="ts">{{.Timestamp.Format "2006-01-02 15:04:05"}}</td>
    </tr>
    {{else}}
    <tr><td colspan="11" class="empty">-- no metrics --</td></tr>
    {{end}}
  </table>


</div>

</div>

<div class="page-footer">
  <div class="page-footer-left">
    <div class="pagination">
      {{range .PageItems}}
        {{if .Ellipsis}}<span class="ellipsis">…</span>{{else if .Current}}<span class="current">{{.Num}}</span>{{else}}<a href="{{.URL}}">{{.Num}}</a>{{end}}
      {{end}}
    </div>
    <span class="meta">page {{.CurrentPage}}/{{.TotalPages}} · {{.TotalMetrics}} records</span>
  </div>
  <div class="build-footer" title="{{commitHash}}">{{shortHash}}</div>
</div>
</body>
</html>`

// ============================================================
// Session detail (text-style)
// ============================================================
const sessionTmpl = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>psb // session {{.SessionID}}</title>
<style>` + sharedCSS + `</style>
` + themeJS + `
</head>
<body>

<div class="header">
  <div class="header-row">
    <h1><span>psb</span> // session detail</h1>
    <button class="theme-toggle" onclick="toggleTheme()">theme</button>
  </div>
  <div class="nav"><a href="/">&larr; back</a></div>
</div>

<div class="section-label">session</div>
<div class="field"><span class="label">session_id</span><span class="val yellow">{{.SessionID}}</span></div>
<div class="field"><span class="label">total records</span><span class="val">{{.Total}}</span></div>
{{if .Env}}
<div class="field"><span class="label">host</span><span class="val">{{.Env.ShortHash}}</span></div>
<div class="field"><span class="label">cpu</span><span class="val">{{.Env.CPUModel}}</span></div>
<div class="field"><span class="label">os</span><span class="val">{{.Env.OS}} {{.Env.KernelVersion}}</span></div>
<div class="field"><span class="label">snakemake</span><span class="val">{{.Env.SnakemakeVersion}}</span></div>
{{end}}

<div class="section-label">records</div>
<table>
  <tr>
    <th>record</th><th>tool</th><th>command</th><th>params</th>
    <th>input</th><th>type</th><th>runtime</th><th>rss_mb</th>
    <th>cpu</th><th>exit</th><th>timestamp</th>
  </tr>
  {{range .Metrics}}
  <tr>
    <td class="record"><a href="/record/{{.RecordID}}">{{.RecordID}}</a></td>
    <td class="tool"><a href="/?tool={{.Tool}}">{{.Tool}}</a></td>
    <td class="cmd">{{.CommandPattern}}</td>
    <td class="param" title="{{.Parameters}}">{{truncate .Parameters 60}}</td>
    <td class="size">{{.InputSizeHuman}}</td>
    <td>{{.InputTypeClean}}</td>
    <td class="time">{{printf "%.2f" .RuntimeSec}}s</td>
    <td>{{printf "%.1f" .MaxRSS}}</td>
    <td>{{printf "%.1f" .AvgCPUPercent}}</td>
    <td {{if eq .ExitCode 0}}class="exit-ok"{{else}}class="exit-fail"{{end}}>{{.ExitCode}}</td>
    <td class="ts">{{.Timestamp.Format "2006-01-02 15:04:05"}}</td>
  </tr>
  {{else}}
  <tr><td colspan="11" class="empty">-- no records --</td></tr>
  {{end}}
</table>


<div style="margin-top: 1rem;">
  <a href="/session/{{.SessionID}}/jsonl" class="theme-toggle" style="text-decoration:none; display:inline-block;">download jsonl</a>
</div>

<div class="page-footer">
  <div class="page-footer-left">
    <div class="pagination">
      {{range .PageItems}}
        {{if .Ellipsis}}<span class="ellipsis">…</span>{{else if .Current}}<span class="current">{{.Num}}</span>{{else}}<a href="{{.URL}}">{{.Num}}</a>{{end}}
      {{end}}
    </div>
    <span class="meta">page {{.CurrentPage}}/{{.TotalPages}} · {{.Total}} records</span>
  </div>
  <div class="build-footer" title="{{commitHash}}">{{shortHash}}</div>
</div>
</body>
</html>`

// ============================================================
// Record detail (text-style, single record)
// ============================================================
const recordTmpl = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>psb // record {{.Metric.RecordID}}</title>
<style>` + sharedCSS + `</style>
` + themeJS + `
</head>
<body>

<div class="header">
  <div class="header-row">
    <h1><span>psb</span> // record detail</h1>
    <button class="theme-toggle" onclick="toggleTheme()">theme</button>
  </div>
  <div class="nav">
    <a href="/">&larr; back</a>
    &middot;
    <a href="/session/{{.Metric.SessionID}}">&larr; session {{.Metric.SessionID}}</a>
  </div>
</div>

<div class="section-label">identification</div>
<div class="field"><span class="label">record_id</span><span class="val">{{.Metric.RecordID}}</span></div>
<div class="field"><span class="label">session_id</span><span class="val yellow"><a href="/session/{{.Metric.SessionID}}">{{.Metric.SessionID}}</a></span></div>

<div class="section-label">environment</div>
<div class="field"><span class="label">host</span><span class="val">{{.Env.ShortHash}}</span></div>
<div class="field"><span class="label">cpu</span><span class="val">{{.Env.CPUModel}}</span></div>
<div class="field"><span class="label">cpu_flags</span><span class="val" style="font-size:10px;word-break:break-all;max-width:600px;">{{if gt (len .Env.CPUFlags) 120}}<span id="cpu-flags-short">{{truncate .Env.CPUFlags 120}} <a href="#" onclick="document.getElementById('cpu-flags-short').style.display='none';document.getElementById('cpu-flags-full').style.display='inline';return false;" style="font-size:10px;color:var(--accent);">[see all]</a></span><span id="cpu-flags-full" style="display:none;word-break:break-all;">{{.Env.CPUFlags}}</span>{{else}}{{.Env.CPUFlags}}{{end}}</span></div>
<div class="field"><span class="label">os</span><span class="val">{{.Env.OS}}</span></div>
<div class="field"><span class="label">kernel</span><span class="val">{{.Env.KernelVersion}}</span></div>
<div class="field"><span class="label">kernel_string</span><span class="val">{{.Env.KernelString}}</span></div>
<div class="field"><span class="label">deploy</span><span class="val">{{.Env.DeployMode}}</span></div>
<div class="field"><span class="label">snakemake</span><span class="val">{{.Env.SnakemakeVersion}}</span></div>

<div class="section-label">execution</div>
<div class="field"><span class="label">tool</span><span class="val accent">{{.Metric.Tool}}</span></div>
<div class="field"><span class="label">command</span><span class="val">{{.Metric.CommandPattern}}</span></div>
<div class="field"><span class="label">params</span><span class="val">{{.Metric.Parameters}}</span></div>

<div class="section-label">i/o</div>
<div class="field"><span class="label">input_size</span><span class="val yellow">{{.Metric.InputSize}} bytes</span></div>
<div class="field"><span class="label">input_type</span><span class="val">{{.Metric.InputType}}</span></div>

<div class="section-label">performance</div>
<div class="field"><span class="label">runtime</span><span class="val green">{{printf "%.2f" .Metric.RuntimeSec}}s</span></div>
<div class="field"><span class="label">max_rss</span><span class="val">{{printf "%.1f" .Metric.MaxRSS}} MB</span></div>
<div class="field"><span class="label">cpu</span><span class="val">{{printf "%.1f" .Metric.AvgCPUPercent}}%</span></div>
<div class="field"><span class="label">exit_code</span><span class="val {{if eq .Metric.ExitCode 0}}green{{else}}red{{end}}">{{.Metric.ExitCode}}</span></div>

<div class="section-label">metadata</div>
<div class="field"><span class="label">timestamp</span><span class="val">{{.Metric.Timestamp.Format "2006-01-02 15:04:05 UTC"}}</span></div>

<div style="margin-top: 2rem;">
  <a href="/record/{{.Metric.RecordID}}/json" class="theme-toggle" style="text-decoration:none; display:inline-block;">download json</a>
</div>

<div class="page-footer">
  <div class="page-footer-left"></div>
  <div class="build-footer" title="{{commitHash}}">{{shortHash}}</div>
</div>
</body>
</html>`

// ============================================================
// Environment detail view
// ============================================================
const envTmpl = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>psb // environment {{.Env.ShortHash}}</title>
<style>` + sharedCSS + `</style>
` + themeJS + `
</head>
<body>

<div class="header">
  <div class="header-row">
    <h1><span>psb</span> // environment detail</h1>
    <button class="theme-toggle" onclick="toggleTheme()">theme</button>
  </div>
  <div class="nav"><a href="/">&larr; back</a></div>
</div>

<div class="section-label">environment</div>
<div class="field"><span class="label">hash</span><span class="val">{{.Env.Hash}}</span></div>
<div class="field"><span class="label">host</span><span class="val">{{.Env.ShortHash}}</span></div>
<div class="field"><span class="label">cpu</span><span class="val">{{.Env.CPUModel}}</span></div>
<div class="field"><span class="label">cpu_flags</span><span class="val" style="font-size:10px;word-break:break-all;max-width:600px;">{{.Env.CPUFlags}}</span></div>
<div class="field"><span class="label">os</span><span class="val">{{.Env.OS}}</span></div>
<div class="field"><span class="label">kernel</span><span class="val">{{.Env.KernelVersion}}</span></div>
<div class="field"><span class="label">kernel_string</span><span class="val">{{.Env.KernelString}}</span></div>
<div class="field"><span class="label">deploy</span><span class="val">{{.Env.DeployMode}}</span></div>
<div class="field"><span class="label">snakemake</span><span class="val">{{.Env.SnakemakeVersion}}</span></div>

<div class="section-label">metrics ({{.Total}})</div>
<table>
  <tr>
    <th>session</th><th>record</th><th>tool</th><th>command</th>
    <th>input</th><th>type</th><th>runtime</th><th>rss_mb</th>
    <th>cpu</th><th>exit</th><th>timestamp</th>
  </tr>
  {{range .Metrics}}
  <tr>
    <td class="session"><a href="/session/{{.SessionID}}">{{.SessionID}}</a></td>
    <td class="record"><a href="/record/{{.RecordID}}">{{.RecordID}}</a></td>
    <td class="tool"><a href="/?tool={{.Tool}}">{{.Tool}}</a></td>
    <td class="cmd">{{.CommandPattern}}</td>
    <td class="size">{{.InputSizeHuman}}</td>
    <td>{{.InputTypeClean}}</td>
    <td class="time">{{printf "%.2f" .RuntimeSec}}s</td>
    <td>{{printf "%.1f" .MaxRSS}}</td>
    <td>{{printf "%.1f" .AvgCPUPercent}}</td>
    <td {{if eq .ExitCode 0}}class="exit-ok"{{else}}class="exit-fail"{{end}}>{{.ExitCode}}</td>
    <td class="ts">{{.Timestamp.Format "2006-01-02 15:04:05"}}</td>
  </tr>
  {{else}}
  <tr><td colspan="11" class="empty">-- no metrics --</td></tr>
  {{end}}
</table>

<div class="page-footer">
  <div class="page-footer-left">
    <div class="pagination">
      {{range .PageItems}}
        {{if .Ellipsis}}<span class="ellipsis">…</span>{{else if .Current}}<span class="current">{{.Num}}</span>{{else}}<a href="{{.URL}}">{{.Num}}</a>{{end}}
      {{end}}
    </div>
    <span class="meta">page {{.CurrentPage}}/{{.TotalPages}} · {{.Total}} records</span>
  </div>
  <div class="build-footer" title="{{commitHash}}">{{shortHash}}</div>
</div>
</body>
</html>`

// ============================================================
// Template compilation
// ============================================================
var templates = template.Must(
	template.New("").Funcs(template.FuncMap{
		"commitHash": func() string { return CommitHash },
		"shortHash": func() string {
			if len(CommitHash) > 7 {
				return CommitHash[:7]
			}
			return CommitHash
		},
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "…"
		},
	}).Parse(viewTmpl +
		`{{define "session"}}` + sessionTmpl + `{{end}}` +
		`{{define "record"}}` + recordTmpl + `{{end}}` +
		`{{define "env"}}` + envTmpl + `{{end}}`),
)

// ============================================================
// Handlers
// ============================================================

// StatItem holds a name/count pair for sidebar aggregations.
type StatItem struct {
	Name  string
	Count int64
	URL   string
}

// PageItem holds a page number and its pre-built URL.
type PageItem struct {
	Num      int
	URL      string
	Current  bool
	Ellipsis bool
}

// windowedPages returns a Google-style paginator: [1] … [4][5][6] … [last]
// urlFn maps a page number to its URL string.
func windowedPages(current, total int, urlFn func(int) string) []PageItem {
	if total <= 1 {
		return []PageItem{{Num: 1, URL: urlFn(1), Current: true}}
	}
	// Collect which page numbers to show
	show := map[int]bool{1: true, total: true}
	for p := current - 2; p <= current+2; p++ {
		if p >= 1 && p <= total {
			show[p] = true
		}
	}
	sorted := make([]int, 0, len(show))
	for p := range show {
		sorted = append(sorted, p)
	}
	// sort
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	var items []PageItem
	prev := 0
	for _, p := range sorted {
		if prev > 0 && p > prev+1 {
			items = append(items, PageItem{Ellipsis: true})
		}
		items = append(items, PageItem{Num: p, URL: urlFn(p), Current: p == current})
		prev = p
	}
	return items
}

// buildFilterURL constructs /?key=val&... from a map, omitting empty values.
func buildFilterURL(params map[string]string) string {
	u := "/"
	sep := "?"
	for _, k := range []string{"tool", "platform", "sm"} {
		if v := params[k]; v != "" {
			u += sep + k + "=" + v
			sep = "&"
		}
	}
	return u
}

func (h *Handler) ViewTelemetry(c echo.Context) error {
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}

	toolFilter := c.QueryParam("tool")
	platformFilter := c.QueryParam("platform")
	smFilter := c.QueryParam("sm")

	// Load all environments for mapping
	var envs []models.Environment
	h.DB.Order("id").Find(&envs)

	envMap := make(map[uint]string, len(envs))
	envHashMap := make(map[uint]string, len(envs))
	for _, e := range envs {
		envMap[e.ID] = e.OS
		envHashMap[e.ID] = e.ShortHash()
	}

	// Resolve platform/sm filters to environment IDs
	var filteredEnvIDs []uint
	hasEnvFilter := platformFilter != "" || smFilter != ""
	if hasEnvFilter {
		eq := h.DB.Model(&models.Environment{})
		if platformFilter != "" {
			eq = eq.Where("os = ?", platformFilter)
		}
		if smFilter != "" {
			eq = eq.Where("snakemake_version = ?", smFilter)
		}
		eq.Pluck("id", &filteredEnvIDs)
	}

	// Build base query
	query := h.DB.Model(&models.ExecutionMetric{}).Where("exit_code = 0")
	if toolFilter != "" {
		query = query.Where("tool = ?", toolFilter)
	}
	if hasEnvFilter {
		query = query.Where("environment_id IN ?", filteredEnvIDs)
	}

	var total int64
	query.Count(&total)
	totalPages := int(math.Ceil(float64(total) / float64(pageSize)))
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	var metrics []models.ExecutionMetric
	q := h.DB.Where("exit_code = 0")
	if toolFilter != "" {
		q = q.Where("tool = ?", toolFilter)
	}
	if hasEnvFilter {
		q = q.Where("environment_id IN ?", filteredEnvIDs)
	}
	q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&metrics)

	// Build page items with pre-computed URLs (windowed, Google-style)
	filterParams := map[string]string{"tool": toolFilter, "platform": platformFilter, "sm": smFilter}
	pageItems := windowedPages(page, totalPages, func(p int) string {
		u := buildFilterURL(filterParams)
		if u == "/" {
			return u + "?page=" + strconv.Itoa(p)
		}
		return u + "&page=" + strconv.Itoa(p)
	})

	// Top platforms by metric count
	type countRow struct {
		OS    string
		Count int64
	}
	var platformRows []countRow
	h.DB.Model(&models.ExecutionMetric{}).
		Select("e.os as os, count(*) as count").
		Joins("JOIN environments e ON e.id = execution_metrics.environment_id").
		Where("exit_code = 0").
		Group("e.os").Order("count DESC").Limit(10).
		Find(&platformRows)
	topPlatforms := make([]StatItem, len(platformRows))
	for i, r := range platformRows {
		topPlatforms[i] = StatItem{
			Name:  r.OS,
			Count: r.Count,
			URL:   buildFilterURL(map[string]string{"tool": toolFilter, "platform": r.OS, "sm": smFilter}),
		}
	}

	// Top snakemake versions by metric count
	type smRow struct {
		SnakemakeVersion string
		Count            int64
	}
	var smRows []smRow
	h.DB.Model(&models.ExecutionMetric{}).
		Select("e.snakemake_version as snakemake_version, count(*) as count").
		Joins("JOIN environments e ON e.id = execution_metrics.environment_id").
		Where("exit_code = 0").
		Group("e.snakemake_version").Order("count DESC").Limit(10).
		Find(&smRows)
	topSnakemake := make([]StatItem, len(smRows))
	for i, r := range smRows {
		topSnakemake[i] = StatItem{
			Name:  r.SnakemakeVersion,
			Count: r.Count,
			URL:   buildFilterURL(map[string]string{"tool": toolFilter, "platform": platformFilter, "sm": r.SnakemakeVersion}),
		}
	}

	// Pre-build clear URLs
	clearToolURL := buildFilterURL(map[string]string{"platform": platformFilter, "sm": smFilter})
	clearPlatformURL := buildFilterURL(map[string]string{"tool": toolFilter, "sm": smFilter})
	clearSmURL := buildFilterURL(map[string]string{"tool": toolFilter, "platform": platformFilter})

	data := map[string]interface{}{
		"EnvMap":           envMap,
		"EnvHashMap":       envHashMap,
		"Metrics":          metrics,
		"CurrentPage":      page,
		"TotalPages":       totalPages,
		"TotalMetrics":     fmt.Sprintf("%d", total),
		"PageItems":        pageItems,
		"ToolFilter":       toolFilter,
		"PlatformFilter":   platformFilter,
		"SmFilter":         smFilter,
		"TopPlatforms":     topPlatforms,
		"TopSnakemake":     topSnakemake,
		"ClearToolURL":     clearToolURL,
		"ClearPlatformURL": clearPlatformURL,
		"ClearSmURL":       clearSmURL,
	}

	c.Response().Header().Set("Content-Type", "text/html")
	return templates.Execute(c.Response().Writer, data)
}

func (h *Handler) ViewSession(c echo.Context) error {
	sessionID := c.Param("id")
	if !safeID.MatchString(sessionID) {
		return c.String(http.StatusBadRequest, "invalid session id")
	}

	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}

	var total int64
	h.DB.Model(&models.ExecutionMetric{}).Where("session_id = ?", sessionID).Count(&total)
	if total == 0 {
		return c.String(http.StatusNotFound, "session not found")
	}

	totalPages := int(math.Ceil(float64(total) / float64(pageSize)))
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	var metrics []models.ExecutionMetric
	h.DB.Where("session_id = ?", sessionID).Order("id ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&metrics)

	// Grab the environment from the first metric
	var env *models.Environment
	if len(metrics) > 0 {
		var e models.Environment
		h.DB.First(&e, metrics[0].EnvironmentID)
		env = &e
	}

	pageItems := windowedPages(page, totalPages, func(p int) string {
		return "?page=" + strconv.Itoa(p)
	})

	data := map[string]interface{}{
		"SessionID":   sessionID,
		"Total":       total,
		"Env":         env,
		"Metrics":     metrics,
		"CurrentPage": page,
		"TotalPages":  totalPages,
		"PageItems":   pageItems,
	}

	c.Response().Header().Set("Content-Type", "text/html")
	return templates.ExecuteTemplate(c.Response().Writer, "session", data)
}

func (h *Handler) ViewRecord(c echo.Context) error {
	recordID := c.Param("id")
	if !safeID.MatchString(recordID) {
		return c.String(http.StatusBadRequest, "invalid record id")
	}

	var metric models.ExecutionMetric
	if err := h.DB.Where("record_id = ?", recordID).First(&metric).Error; err != nil {
		return c.String(http.StatusNotFound, "record not found")
	}

	var env models.Environment
	h.DB.First(&env, metric.EnvironmentID)

	data := map[string]interface{}{
		"Metric": metric,
		"Env":    env,
	}

	c.Response().Header().Set("Content-Type", "text/html")
	return templates.ExecuteTemplate(c.Response().Writer, "record", data)
}

func (h *Handler) DownloadRecordJSON(c echo.Context) error {
	recordID := c.Param("id")
	if !safeID.MatchString(recordID) {
		return c.String(http.StatusBadRequest, "invalid record id")
	}

	var metric models.ExecutionMetric
	if err := h.DB.Where("record_id = ?", recordID).First(&metric).Error; err != nil {
		return c.String(http.StatusNotFound, "record not found")
	}

	var env models.Environment
	h.DB.First(&env, metric.EnvironmentID)

	payload := map[string]interface{}{
		"metric":      metric,
		"environment": env,
	}

	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="record-%s.json"`, recordID))
	return c.JSON(http.StatusOK, payload)
}

func (h *Handler) ViewEnv(c echo.Context) error {
	idParam := c.Param("id")
	envID, err := strconv.Atoi(idParam)
	if err != nil || envID < 1 {
		return c.String(http.StatusBadRequest, "invalid environment id")
	}

	var env models.Environment
	if err := h.DB.First(&env, envID).Error; err != nil {
		return c.String(http.StatusNotFound, "environment not found")
	}

	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}

	var total int64
	h.DB.Model(&models.ExecutionMetric{}).Where("environment_id = ?", envID).Count(&total)
	totalPages := int(math.Ceil(float64(total) / float64(pageSize)))
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	var metrics []models.ExecutionMetric
	h.DB.Where("environment_id = ?", envID).Order("id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&metrics)

	pageItems := windowedPages(page, totalPages, func(p int) string {
		return "?page=" + strconv.Itoa(p)
	})

	data := map[string]interface{}{
		"Env":         env,
		"Total":       total,
		"Metrics":     metrics,
		"CurrentPage": page,
		"TotalPages":  totalPages,
		"PageItems":   pageItems,
	}

	c.Response().Header().Set("Content-Type", "text/html")
	return templates.ExecuteTemplate(c.Response().Writer, "env", data)
}

func (h *Handler) DownloadSessionJSONL(c echo.Context) error {
	sessionID := c.Param("id")
	if !safeID.MatchString(sessionID) {
		return c.String(http.StatusBadRequest, "invalid session id")
	}

	var metrics []models.ExecutionMetric
	h.DB.Where("session_id = ?", sessionID).Order("id ASC").Find(&metrics)
	if len(metrics) == 0 {
		return c.String(http.StatusNotFound, "session not found")
	}

	// Build env lookup
	envIDs := make(map[uint]bool)
	for _, m := range metrics {
		envIDs[m.EnvironmentID] = true
	}
	ids := make([]uint, 0, len(envIDs))
	for id := range envIDs {
		ids = append(ids, id)
	}
	var envs []models.Environment
	h.DB.Where("id IN ?", ids).Find(&envs)
	envMap := make(map[uint]models.Environment, len(envs))
	for _, e := range envs {
		envMap[e.ID] = e
	}

	c.Response().Header().Set("Content-Type", "application/x-ndjson")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="session-%s.jsonl"`, sessionID))
	c.Response().WriteHeader(http.StatusOK)

	enc := json.NewEncoder(c.Response().Writer)
	for _, m := range metrics {
		line := map[string]interface{}{
			"metric":      m,
			"environment": envMap[m.EnvironmentID],
		}
		enc.Encode(line)
	}
	return nil
}
