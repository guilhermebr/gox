package postgres

// parseSQL extracts operation and table name from SQL query for metrics
func parseSQL(sql string) (operation, table string) {
	// This is a simplified parser for basic SQL operations
	// In production, you might want to use a more sophisticated SQL parser

	// Default values
	operation = "unknown"
	table = "unknown"

	// Remove leading/trailing whitespace and convert to lowercase
	sql = trimAndLower(sql)

	// Extract operation (first word)
	if len(sql) > 0 {
		words := splitWords(sql)
		if len(words) > 0 {
			operation = words[0]
		}

		// Extract table name based on operation
		switch operation {
		case "select":
			table = extractTableFromSelect(words)
		case "insert":
			table = extractTableFromInsert(words)
		case "update":
			table = extractTableFromUpdate(words)
		case "delete":
			table = extractTableFromDelete(words)
		default:
			if len(words) > 1 {
				table = words[1]
			}
		}
	}

	return operation, table
}

// Helper functions for SQL parsing
func trimAndLower(s string) string {
	// Simple trim and lowercase function
	start := 0
	end := len(s)

	// Trim leading whitespace
	for start < end && isWhitespace(s[start]) {
		start++
	}

	// Trim trailing whitespace
	for end > start && isWhitespace(s[end-1]) {
		end--
	}

	// Convert to lowercase
	result := make([]byte, end-start)
	for i := start; i < end; i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			result[i-start] = s[i] + 32
		} else {
			result[i-start] = s[i]
		}
	}

	return string(result)
}

func isWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func splitWords(s string) []string {
	var words []string
	var current []byte

	for i := 0; i < len(s); i++ {
		if isWhitespace(s[i]) {
			if len(current) > 0 {
				words = append(words, string(current))
				current = current[:0]
			}
		} else {
			current = append(current, s[i])
		}
	}

	if len(current) > 0 {
		words = append(words, string(current))
	}

	return words
}

func extractTableFromSelect(words []string) string {
	for i, word := range words {
		if word == "from" && i+1 < len(words) {
			return words[i+1]
		}
	}
	return "unknown"
}

func extractTableFromInsert(words []string) string {
	for i, word := range words {
		if word == "into" && i+1 < len(words) {
			return words[i+1]
		}
	}
	return "unknown"
}

func extractTableFromUpdate(words []string) string {
	if len(words) > 1 {
		return words[1]
	}
	return "unknown"
}

func extractTableFromDelete(words []string) string {
	for i, word := range words {
		if word == "from" && i+1 < len(words) {
			return words[i+1]
		}
	}
	return "unknown"
}
