package writeback

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sadmachine/asanamate/internal/asana"
)

var escapeHTML = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// writeMentions escapes text and replaces unambiguous @Full Name references.
func writeMentions(b *strings.Builder, text string, users []asana.Ref) (mentioned []asana.Ref) {
	plain := 0
	for i := 0; i < len(text); i++ {
		if text[i] != '@' || wordRuneBefore(text, i) {
			continue
		}
		u, n, ok := matchUser(text[i+1:], users)
		if !ok {
			continue
		}
		b.WriteString(escapeHTML.Replace(text[plain:i]))
		b.WriteString(`<a data-asana-gid="` + u.GID + `"/>`)
		mentioned = append(mentioned, u)
		plain = i + 1 + n
		i = plain - 1
	}
	b.WriteString(escapeHTML.Replace(text[plain:]))
	return mentioned
}

// matchUser finds the user whose longest name starts rest and ends on a word
// boundary, returning the name's length in rest. A tie is ambiguous.
func matchUser(rest string, users []asana.Ref) (user asana.Ref, n int, ok bool) {
	for _, u := range users {
		name := strings.TrimSpace(u.Name)
		l := len(name)
		if l == 0 || !asana.ValidGID(u.GID) || l < n || l > len(rest) || !strings.EqualFold(rest[:l], name) || wordRuneAt(rest, l) {
			continue
		}
		if l == n {
			ok = ok && user.GID == u.GID
			continue
		}
		user, n, ok = u, l, true
	}
	return user, n, ok
}

func wordRuneBefore(s string, i int) bool {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return i > 0 && isWord(r)
}

func wordRuneAt(s string, i int) bool {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return i < len(s) && isWord(r)
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
