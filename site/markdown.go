package main

import (
	"bytes"
	"html"
	"regexp"
	"strings"
)

// Markdown renders the subset of Markdown that MATRIX.md uses: headings,
// paragraphs, tables, inline code, bold and links. HTML comments are
// dropped. Every other line renders as an escaped paragraph, so nothing is
// lost and no raw HTML passes through.
func Markdown(src []byte) []byte {
	var out bytes.Buffer
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	var para []string
	flush := func() {
		if len(para) > 0 {
			out.WriteString("<p>" + inline(strings.Join(para, " ")) + "</p>\n")
			para = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "<!--") && strings.HasSuffix(line, "-->"):
			flush()
		case heading.MatchString(line):
			flush()
			m := heading.FindStringSubmatch(line)
			n := string(rune('0' + len(m[1])))
			out.WriteString("<h" + n + ">" + inline(m[2]) + "</h" + n + ">\n")
		case strings.HasPrefix(line, "|") && i+1 < len(lines) && separator.MatchString(strings.TrimSpace(lines[i+1])):
			flush()
			var rows [][]string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				rows = append(rows, cells(lines[i]))
			}
			i--
			writeTable(&out, rows[0], rows[2:])
		default:
			para = append(para, line)
		}
	}
	flush()
	return out.Bytes()
}

var (
	heading   = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	separator = regexp.MustCompile(`^\|(\s*:?-+:?\s*\|)+$`)
	bold      = regexp.MustCompile(`\*\*(.+?)\*\*`)
	link      = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
)

func cells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func writeTable(out *bytes.Buffer, head []string, body [][]string) {
	out.WriteString("<table>\n<thead><tr>")
	for _, c := range head {
		out.WriteString("<th>" + inline(c) + "</th>")
	}
	out.WriteString("</tr></thead>\n<tbody>\n")
	for _, row := range body {
		out.WriteString("<tr>")
		for _, c := range row {
			out.WriteString("<td>" + inline(c) + "</td>")
		}
		out.WriteString("</tr>\n")
	}
	out.WriteString("</tbody>\n</table>\n")
}

// inline escapes text, then renders code spans, bold and links. Code spans
// are split out first, so their contents stay literal.
func inline(s string) string {
	parts := strings.Split(s, "`")
	var b strings.Builder
	for i, part := range parts {
		if i%2 == 1 && i < len(parts)-1 {
			b.WriteString("<code>" + html.EscapeString(part) + "</code>")
			continue
		}
		if i%2 == 1 {
			// An unmatched backtick stays as text.
			b.WriteString("`")
		}
		text := html.EscapeString(part)
		text = bold.ReplaceAllString(text, "<strong>$1</strong>")
		text = link.ReplaceAllStringFunc(text, func(m string) string {
			sub := link.FindStringSubmatch(m)
			if !strings.HasPrefix(sub[2], "https://") && !strings.HasPrefix(sub[2], "http://") && !strings.HasPrefix(sub[2], "#") {
				return sub[1]
			}
			return `<a href="` + sub[2] + `">` + sub[1] + `</a>`
		})
		b.WriteString(text)
	}
	return b.String()
}
