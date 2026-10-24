package brick

import (
	"strings"
)

// normalizeLocale canonicalizes a BCP 47-style tag (E9): `pt_BR` → `pt-BR`,
// `EN` → `en`, `zh-hant-tw` → `zh-Hant-TW`. It accepts a language (2–3
// letters), an optional script (4 letters) and an optional region (2 letters
// or 3 digits), which covers every tag a file-name suffix needs.
func normalizeLocale(tag string) (string, bool) {
	parts := strings.Split(strings.ReplaceAll(tag, "_", "-"), "-")
	if len(parts) == 0 || len(parts) > 3 {
		return "", false
	}
	lang := strings.ToLower(parts[0])
	if !isLetters(lang) || len(lang) < 2 || len(lang) > 3 {
		return "", false
	}
	out := []string{lang}
	rest := parts[1:]
	if len(rest) > 0 && len(rest[0]) == 4 && isLetters(rest[0]) {
		s := strings.ToLower(rest[0])
		out = append(out, strings.ToUpper(s[:1])+s[1:])
		rest = rest[1:]
	}
	if len(rest) > 0 {
		r := rest[0]
		switch {
		case len(r) == 2 && isLetters(r):
			out = append(out, strings.ToUpper(r))
		case len(r) == 3 && isDigits(r):
			out = append(out, r)
		default:
			return "", false
		}
		rest = rest[1:]
	}
	if len(rest) > 0 {
		return "", false
	}
	return strings.Join(out, "-"), true
}

// localeChain is the fallback order for a tag: `zh-Hant-TW` → `zh-Hant-TW`,
// `zh-Hant`, `zh`.
func localeChain(tag string) []string {
	var out []string
	for tag != "" {
		out = append(out, tag)
		i := strings.LastIndexByte(tag, '-')
		if i < 0 {
			break
		}
		tag = tag[:i]
	}
	return out
}

func isLetters(s string) bool {
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return s != ""
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// NormalizeLocale canonicalizes a language tag the way brick does for file
// variants and WithLocale (`pt_br` → `pt-BR`); ok is false for a string that
// is not a tag. Exported so layers above the engine agree with it exactly.
func NormalizeLocale(tag string) (string, bool) { return normalizeLocale(tag) }
