// Package prompt reads answers to line-based terminal questions.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Line asks for one line of input. An empty answer returns def.
func Line(in *bufio.Reader, out io.Writer, label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	s, err := in.ReadString('\n')
	if err != nil && (err != io.EOF || s == "") {
		return "", err
	}
	if s = strings.TrimSpace(s); s == "" {
		return def, nil
	}
	return s, nil
}

// Confirm asks a yes/no question. Only "y" or "yes" count as yes.
func Confirm(in *bufio.Reader, out io.Writer, question string) (bool, error) {
	answer, err := Line(in, out, question+" [y/N]", "")
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes", nil
}
