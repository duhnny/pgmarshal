package utils

import "regexp"

func InterpolateMap(s string, m map[string]string) string {
	getTags := regexp.MustCompile(`\$\{[^\$\{\}]+\}`)
	getKeys := regexp.MustCompile(`^\$\{([^\$\{\}]+)\}$`)

	out := getTags.ReplaceAllStringFunc(s, func(tag string) string {
		matches := getKeys.FindStringSubmatch(tag)
		key := matches[1]
		
		return m[key]
	})

	return out
}
