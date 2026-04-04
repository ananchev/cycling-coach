package reporting

import (
	"bytes"
	"fmt"
	"html/template"
	"regexp"
	"strings"
	"time"

	"cycling-coach/internal/storage"
)

var reportTemplate = template.Must(template.New("report").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}}</title>
  <style>
    *{box-sizing:border-box;margin:0;padding:0}
    body{font-family:system-ui,sans-serif;background:#f5f5f5;color:#222;padding:24px}
    .report{max-width:800px;margin:0 auto;background:#fff;border:1px solid #ddd;border-radius:8px;padding:32px}
    header{margin-bottom:24px;border-bottom:2px solid #e5e7eb;padding-bottom:16px}
    h1{font-size:1.5rem;margin-bottom:6px}
    .period{color:#666;font-size:.9rem}
    .summary{background:#f0f9ff;border-left:4px solid #2563eb;padding:16px 20px;border-radius:4px;margin-bottom:24px}
    .summary h2{font-size:.85rem;text-transform:uppercase;letter-spacing:.05em;color:#2563eb;margin-bottom:8px}
    .summary pre{white-space:pre-wrap;word-wrap:break-word;font-family:inherit;font-size:.95rem;line-height:1.6}
    .narrative{line-height:1.7;font-size:.95rem}
    .narrative h1,.narrative h2,.narrative h3{margin:20px 0 10px;font-size:1.1rem;color:#111}
    .narrative h1{font-size:1.3rem}
    .narrative p{margin-bottom:12px}
    .narrative ul,.narrative ol{margin:0 0 12px 24px}
    .narrative li{margin-bottom:4px}
    .narrative strong,.narrative b{font-weight:600}
    .narrative em,.narrative i{font-style:italic}
    .narrative table{width:100%;border-collapse:collapse;margin-bottom:16px;font-size:.88rem}
    .narrative th{text-align:left;padding:7px 10px;background:#f3f4f6;border-bottom:2px solid #e5e7eb}
    .narrative td{padding:6px 10px;border-bottom:1px solid #f0f0f0}
    .narrative hr{border:none;border-top:1px solid #e5e7eb;margin:20px 0}
    .narrative code{background:#f3f4f6;padding:1px 5px;border-radius:3px;font-size:.88em}
    .narrative pre{background:#f3f4f6;padding:12px 16px;border-radius:4px;overflow-x:auto;margin-bottom:12px}
    @media(max-width:600px){body{padding:12px}.report{padding:16px}}
  </style>
</head>
<body>
  <article class="report">
    <header>
      <h1>{{.Title}}</h1>
      <p class="period">Period: {{.PeriodRange}}</p>
    </header>
    <section class="summary">
      <h2>Coaching Summary</h2>
      <pre>{{.Summary}}</pre>
    </section>
    <section class="narrative">
      {{.NarrativeHTML}}
    </section>
  </article>
</body>
</html>
`))

type reportTemplateData struct {
	Title         string
	PeriodRange   string
	Summary       string
	NarrativeHTML template.HTML
}

// RenderHTML produces a complete HTML page for the given report output.
// narrative is treated as pre-formatted markdown-like text rendered inside a <pre> block
// until a proper markdown renderer is wired in a later phase.
func RenderHTML(reportType storage.ReportType, weekStart, weekEnd time.Time, output *ReportOutput) (string, error) {
	title := "Training Report"
	if reportType == storage.ReportTypeWeeklyPlan {
		title = "Training Plan"
	}

	weekRange := fmt.Sprintf("%s – %s",
		weekStart.Format("2 Jan 2006"),
		weekEnd.Format("2 Jan 2006"),
	)

	narrativeHTML := template.HTML(markdownToHTML(output.Narrative)) //nolint:gosec // output is Claude-generated, not user input; escaping is done inside markdownToHTML

	data := reportTemplateData{
		Title:         title,
		PeriodRange:   weekRange,
		Summary:       output.Summary,
		NarrativeHTML: narrativeHTML,
	}

	var buf bytes.Buffer
	if err := reportTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("reporting.RenderHTML: execute template: %w", err)
	}
	return buf.String(), nil
}

// markdownToHTML converts the subset of markdown that Claude produces into safe HTML.
// All input is HTML-escaped first, then structural patterns are applied.
func markdownToHTML(md string) string {
	// Escape HTML entities first to prevent injection.
	escaped := template.HTMLEscapeString(md)

	lines := strings.Split(escaped, "\n")
	var out strings.Builder
	inUL := false
	inOL := false

	olNum := regexp.MustCompile(`^(\d+)\. (.+)$`)
	hrPat := regexp.MustCompile(`^[-*_]{3,}$`)

	flush := func() {
		if inUL {
			out.WriteString("</ul>\n")
			inUL = false
		}
		if inOL {
			out.WriteString("</ol>\n")
			inOL = false
		}
	}

	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")

		switch {
		case strings.HasPrefix(line, "### "):
			flush()
			out.WriteString("<h3>" + inlineMarkdown(line[4:]) + "</h3>\n")
		case strings.HasPrefix(line, "## "):
			flush()
			out.WriteString("<h2>" + inlineMarkdown(line[3:]) + "</h2>\n")
		case strings.HasPrefix(line, "# "):
			flush()
			out.WriteString("<h1>" + inlineMarkdown(line[2:]) + "</h1>\n")
		case hrPat.MatchString(line):
			flush()
			out.WriteString("<hr>\n")
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			if !inUL {
				flush()
				out.WriteString("<ul>\n")
				inUL = true
			}
			out.WriteString("<li>" + inlineMarkdown(line[2:]) + "</li>\n")
		case olNum.MatchString(line):
			m := olNum.FindStringSubmatch(line)
			if !inOL {
				flush()
				out.WriteString("<ol>\n")
				inOL = true
			}
			out.WriteString("<li>" + inlineMarkdown(m[2]) + "</li>\n")
		case line == "":
			flush()
			out.WriteString("\n")
		default:
			flush()
			out.WriteString("<p>" + inlineMarkdown(line) + "</p>\n")
		}
	}
	flush()
	return out.String()
}

// RenderMarkdownFragment converts markdown-like coaching text into safe HTML
// fragments for inline admin rendering.
func RenderMarkdownFragment(md string) string {
	return markdownToHTML(md)
}

// inlineMarkdown handles **bold**, *italic*, and `code` within a single line.
// The input is already HTML-escaped.
var (
	reBold   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic = regexp.MustCompile(`\*(.+?)\*`)
	reCode   = regexp.MustCompile("`(.+?)`")
)

func inlineMarkdown(s string) string {
	s = reBold.ReplaceAllString(s, "<strong>$1</strong>")
	s = reItalic.ReplaceAllString(s, "<em>$1</em>")
	s = reCode.ReplaceAllString(s, "<code>$1</code>")
	return s
}
