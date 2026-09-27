package telegram

import (
	"html"
	"regexp"
	"strings"
)

// Telegram supports only a small HTML subset (b, i, s, code, pre, a,
// blockquote), so model Markdown is converted line by line: fenced code
// becomes <pre>, headings become bold, list markers become bullets and
// tables are kept monospaced.

var (
	headingRe    = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	listItemRe   = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	inlineCodeRe = regexp.MustCompile("`([^`\n]+)`")
	linkRe       = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^\s)]+)\)`)
	boldRe       = regexp.MustCompile(`\*\*([^*\n]+?)\*\*|__([^_\n]+?)__`)
	strikeRe     = regexp.MustCompile(`~~([^~\n]+?)~~`)
	italicRe     = regexp.MustCompile(`(^|[^*\w])\*([^*\s][^*\n]*?)\*`)
	hrRe         = regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`)
	codeLangRe   = regexp.MustCompile(`^[\w+#.-]+$`)
)

func markdownToHTML(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			code := make([]string, 0)
			for i+1 < len(lines) {
				i++
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
					break
				}
				code = append(code, lines[i])
			}
			out = append(out, preBlock(strings.Join(code, "\n"), lang))
			continue
		}

		if strings.HasPrefix(trimmed, "|") {
			table := []string{line}
			for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "|") {
				i++
				table = append(table, lines[i])
			}
			out = append(out, preBlock(strings.Join(table, "\n"), ""))
			continue
		}

		if strings.HasPrefix(trimmed, ">") {
			quote := []string{formatInline(strings.TrimSpace(strings.TrimPrefix(trimmed, ">")))}
			for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), ">") {
				i++
				next := strings.TrimPrefix(strings.TrimSpace(lines[i]), ">")
				quote = append(quote, formatInline(strings.TrimSpace(next)))
			}
			out = append(out, "<blockquote>"+strings.Join(quote, "\n")+"</blockquote>")
			continue
		}

		if hrRe.MatchString(line) {
			out = append(out, "──────────")
			continue
		}
		if m := headingRe.FindStringSubmatch(trimmed); m != nil {
			out = append(out, "<b>"+formatInline(strings.Trim(m[1], "*_ "))+"</b>")
			continue
		}
		if m := listItemRe.FindStringSubmatch(line); m != nil {
			out = append(out, m[1]+"• "+formatInline(m[2]))
			continue
		}
		out = append(out, formatInline(line))
	}

	return strings.Join(out, "\n")
}

func preBlock(code, lang string) string {
	escaped := html.EscapeString(code)
	if lang != "" && codeLangRe.MatchString(lang) {
		return `<pre><code class="language-` + lang + `">` + escaped + "</code></pre>"
	}
	return "<pre>" + escaped + "</pre>"
}

// formatInline converts inline Markdown; code spans are cut out first so
// their content is never treated as formatting.
func formatInline(s string) string {
	var b strings.Builder
	last := 0
	for _, loc := range inlineCodeRe.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(formatText(s[last:loc[0]]))
		b.WriteString("<code>" + html.EscapeString(s[loc[2]:loc[3]]) + "</code>")
		last = loc[1]
	}
	b.WriteString(formatText(s[last:]))
	return b.String()
}

func formatText(s string) string {
	s = html.EscapeString(s)
	s = linkRe.ReplaceAllString(s, `<a href="$2">$1</a>`)
	s = boldRe.ReplaceAllString(s, "<b>$1$2</b>")
	s = strikeRe.ReplaceAllString(s, "<s>$1</s>")
	s = italicRe.ReplaceAllString(s, "$1<i>$2</i>")
	return s
}
