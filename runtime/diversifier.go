package runtime

import (
	"fmt"
	"hash/fnv"
	"net/url"
	"sort"
	"strings"
)

func RequestDiversifier(policy Policy, endpointName string, query url.Values, pathVars map[string]string) string {
	var parts []string

	if enabled, _ := policy.PathVariantEnabled(endpointName); enabled {
		if p := PathDiversifier(varsToValues(pathVars)); p != "" {
			parts = append(parts, p)
		}
	}
	if enabled, _ := policy.QueryVariantEnabled(endpointName); enabled {
		if q := QueryDiversifier(query); q != "" {
			parts = append(parts, q)
		}
	}
	return strings.Join(parts, "--")
}

func varsToValues(vars map[string]string) url.Values {
	if len(vars) == 0 {
		return nil
	}
	out := url.Values{}
	for k, v := range vars {
		out.Add(k, v)
	}
	return out
}

func QueryDiversifier(values url.Values) string {
	normalized := NormalizeValues(values)
	if normalized == "" {
		return ""
	}
	return "q-" + hash64Hex(normalized)
}

func PathDiversifier(values url.Values) string {
	normalized := NormalizeValues(values)
	if normalized == "" {
		return ""
	}
	return "p-" + hash64Hex(normalized)
}

func NormalizeValues(values url.Values) string {
	if len(values) == 0 {
		return ""
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	first := true
	for _, key := range keys {
		vals := append([]string(nil), values[key]...)
		if len(vals) == 0 {
			vals = []string{""}
		}
		sort.Strings(vals)
		for _, val := range vals {
			if !first {
				b.WriteByte('&')
			}
			first = false
			b.WriteString(url.QueryEscape(key))
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(val))
		}
	}
	return b.String()
}

func hash64Hex(value string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(value))
	return fmt.Sprintf("%016x", h.Sum64())
}

