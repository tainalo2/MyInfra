package store

import (
	"strings"
	"unicode"
)

func splitSQL(sql string) []string {
	var out []string
	var b strings.Builder
	inSingle := false
	dollar := ""
	for i := 0; i < len(sql); i++ {
		if dollar != "" {
			if strings.HasPrefix(sql[i:], dollar) {
				b.WriteString(dollar)
				i += len(dollar) - 1
				dollar = ""
				continue
			}
			b.WriteByte(sql[i])
			continue
		}
		if inSingle {
			b.WriteByte(sql[i])
			if sql[i] == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					b.WriteByte('\'')
					i++
					continue
				}
				inSingle = false
			}
			continue
		}
		if sql[i] == '\'' {
			inSingle = true
			b.WriteByte(sql[i])
			continue
		}
		if sql[i] == '$' {
			j := i + 1
			for j < len(sql) && (unicode.IsLetter(rune(sql[j])) || unicode.IsDigit(rune(sql[j])) || sql[j] == '_') {
				j++
			}
			if j < len(sql) && sql[j] == '$' {
				dollar = sql[i : j+1]
				b.WriteString(dollar)
				i = j
				continue
			}
		}
		if sql[i] == ';' {
			if stmt := strings.TrimSpace(b.String()); stmt != "" {
				out = append(out, stmt)
			}
			b.Reset()
			continue
		}
		b.WriteByte(sql[i])
	}
	if stmt := strings.TrimSpace(b.String()); stmt != "" {
		out = append(out, stmt)
	}
	return out
}
