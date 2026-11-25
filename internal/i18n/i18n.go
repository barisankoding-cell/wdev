package i18n

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const (
	// LocaleEN is the default English locale.
	LocaleEN = "en"
	// LocaleID is the Indonesian locale.
	LocaleID = "id"
	// DefaultLocale is always English ("en").
	DefaultLocale = LocaleEN
)

type langCandidate struct {
	locale string
	q      float64
	order  int
}

// Normalize parses a locale tag or Accept-Language header value and returns
// either "en" or "id", defaulting to DefaultLocale ("en").
func Normalize(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return DefaultLocale
	}

	var candidates []langCandidate
	parts := strings.Split(s, ",")
	for idx, part := range parts {
		sub := strings.Split(part, ";")
		tag := strings.TrimSpace(strings.ReplaceAll(sub[0], "_", "-"))
		if tag == "" || tag == "*" {
			continue
		}
		q := 1.0
		for _, param := range sub[1:] {
			kv := strings.SplitN(strings.TrimSpace(param), "=", 2)
			if len(kv) == 2 && strings.TrimSpace(kv[0]) == "q" {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64); err == nil {
					q = parsed
				}
			}
		}
		if q <= 0 {
			continue
		}
		primary := strings.SplitN(tag, "-", 2)[0]
		if primary == LocaleEN || primary == LocaleID {
			candidates = append(candidates, langCandidate{
				locale: primary,
				q:      q,
				order:  idx,
			})
		}
	}

	if len(candidates) == 0 {
		return DefaultLocale
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].q != candidates[j].q {
			return candidates[i].q > candidates[j].q
		}
		return candidates[i].order < candidates[j].order
	})
	return candidates[0].locale
}

// FromRequest resolves locale from `?lang=` query parameter first, then the
// `Accept-Language` header, defaulting to "en".
func FromRequest(r *http.Request) string {
	if r == nil {
		return DefaultLocale
	}
	if r.URL != nil {
		if q := strings.TrimSpace(r.URL.Query().Get("lang")); q != "" {
			return Normalize(q)
		}
	}
	return Normalize(r.Header.Get("Accept-Language"))
}

// Pick returns idText when locale normalizes to "id", and enText otherwise.
func Pick(locale, enText, idText string) string {
	if Normalize(locale) == LocaleID {
		return idText
	}
	return enText
}
