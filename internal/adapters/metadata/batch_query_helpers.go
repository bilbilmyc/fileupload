package metadata

import "fmt"

const batchQueryLimit = 500

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func sqlitePlaceholders(count int) string {
	placeholders := make([]string, count)
	for i := range placeholders {
		placeholders[i] = "?"
	}
	return joinPlaceholders(placeholders)
}

func postgresPlaceholders(count int) string {
	placeholders := make([]string, count)
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	return joinPlaceholders(placeholders)
}

func joinPlaceholders(placeholders []string) string {
	if len(placeholders) == 0 {
		return ""
	}
	result := placeholders[0]
	for _, placeholder := range placeholders[1:] {
		result += ", " + placeholder
	}
	return result
}
