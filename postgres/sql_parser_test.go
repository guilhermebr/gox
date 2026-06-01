package postgres

import "testing"

func TestParseSQL(t *testing.T) {
	tests := []struct {
		name              string
		sql               string
		expectedOperation string
		expectedTable     string
	}{
		{
			name:              "Simple SELECT",
			sql:               "SELECT * FROM users",
			expectedOperation: "select",
			expectedTable:     "users",
		},
		{
			name:              "SELECT with WHERE",
			sql:               "SELECT id, name FROM users WHERE active = true",
			expectedOperation: "select",
			expectedTable:     "users",
		},
		{
			name:              "INSERT INTO",
			sql:               "INSERT INTO users (name, email) VALUES ($1, $2)",
			expectedOperation: "insert",
			expectedTable:     "users",
		},
		{
			name:              "UPDATE statement",
			sql:               "UPDATE users SET name = $1 WHERE id = $2",
			expectedOperation: "update",
			expectedTable:     "users",
		},
		{
			name:              "DELETE statement",
			sql:               "DELETE FROM users WHERE id = $1",
			expectedOperation: "delete",
			expectedTable:     "users",
		},
		{
			name:              "Complex SELECT with JOIN",
			sql:               "SELECT u.name, p.title FROM users u JOIN posts p ON u.id = p.user_id",
			expectedOperation: "select",
			expectedTable:     "users",
		},
		{
			name:              "Case insensitive",
			sql:               "select * from PRODUCTS",
			expectedOperation: "select",
			expectedTable:     "products",
		},
		{
			name:              "With extra whitespace",
			sql:               "  SELECT   *   FROM   orders  ",
			expectedOperation: "select",
			expectedTable:     "orders",
		},
		{
			name:              "CREATE TABLE",
			sql:               "CREATE TABLE users (id SERIAL PRIMARY KEY)",
			expectedOperation: "create",
			expectedTable:     "table",
		},
		{
			name:              "DROP TABLE",
			sql:               "DROP TABLE users",
			expectedOperation: "drop",
			expectedTable:     "table",
		},
		{
			name:              "Empty string",
			sql:               "",
			expectedOperation: "unknown",
			expectedTable:     "unknown",
		},
		{
			name:              "Malformed SQL",
			sql:               "NONSENSE QUERY",
			expectedOperation: "nonsense",
			expectedTable:     "query",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation, table := parseSQL(tt.sql)
			if operation != tt.expectedOperation {
				t.Errorf("Expected operation %q, got %q", tt.expectedOperation, operation)
			}
			if table != tt.expectedTable {
				t.Errorf("Expected table %q, got %q", tt.expectedTable, table)
			}
		})
	}
}

func TestTrimAndLower(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Normal string",
			input:    "Hello World",
			expected: "hello world",
		},
		{
			name:     "With leading/trailing spaces",
			input:    "  Hello World  ",
			expected: "hello world",
		},
		{
			name:     "With tabs and newlines",
			input:    "\t\nHello World\r\n",
			expected: "hello world",
		},
		{
			name:     "All uppercase",
			input:    "HELLO WORLD",
			expected: "hello world",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Only whitespace",
			input:    "   \t\n  ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := trimAndLower(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSplitWords(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Simple words",
			input:    "hello world",
			expected: []string{"hello", "world"},
		},
		{
			name:     "Multiple spaces",
			input:    "hello   world",
			expected: []string{"hello", "world"},
		},
		{
			name:     "With tabs",
			input:    "hello\tworld",
			expected: []string{"hello", "world"},
		},
		{
			name:     "Empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "Single word",
			input:    "hello",
			expected: []string{"hello"},
		},
		{
			name:     "Leading/trailing spaces",
			input:    "  hello world  ",
			expected: []string{"hello", "world"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitWords(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d words, got %d", len(tt.expected), len(result))
				return
			}
			for i, word := range result {
				if word != tt.expected[i] {
					t.Errorf("Expected word %d to be %q, got %q", i, tt.expected[i], word)
				}
			}
		})
	}
}

func TestExtractTableFunctions(t *testing.T) {
	t.Run("extractTableFromSelect", func(t *testing.T) {
		tests := []struct {
			words    []string
			expected string
		}{
			{[]string{"select", "*", "from", "users"}, "users"},
			{[]string{"select", "id", "from", "products", "where"}, "products"},
			{[]string{"select", "*"}, "unknown"},
			{[]string{}, "unknown"},
		}

		for _, tt := range tests {
			result := extractTableFromSelect(tt.words)
			if result != tt.expected {
				t.Errorf("extractTableFromSelect(%v) = %q, want %q", tt.words, result, tt.expected)
			}
		}
	})

	t.Run("extractTableFromInsert", func(t *testing.T) {
		tests := []struct {
			words    []string
			expected string
		}{
			{[]string{"insert", "into", "users", "values"}, "users"},
			{[]string{"insert", "into", "products"}, "products"},
			{[]string{"insert"}, "unknown"},
			{[]string{}, "unknown"},
		}

		for _, tt := range tests {
			result := extractTableFromInsert(tt.words)
			if result != tt.expected {
				t.Errorf("extractTableFromInsert(%v) = %q, want %q", tt.words, result, tt.expected)
			}
		}
	})

	t.Run("extractTableFromUpdate", func(t *testing.T) {
		tests := []struct {
			words    []string
			expected string
		}{
			{[]string{"update", "users", "set"}, "users"},
			{[]string{"update", "products"}, "products"},
			{[]string{"update"}, "unknown"},
			{[]string{}, "unknown"},
		}

		for _, tt := range tests {
			result := extractTableFromUpdate(tt.words)
			if result != tt.expected {
				t.Errorf("extractTableFromUpdate(%v) = %q, want %q", tt.words, result, tt.expected)
			}
		}
	})

	t.Run("extractTableFromDelete", func(t *testing.T) {
		tests := []struct {
			words    []string
			expected string
		}{
			{[]string{"delete", "from", "users", "where"}, "users"},
			{[]string{"delete", "from", "products"}, "products"},
			{[]string{"delete"}, "unknown"},
			{[]string{}, "unknown"},
		}

		for _, tt := range tests {
			result := extractTableFromDelete(tt.words)
			if result != tt.expected {
				t.Errorf("extractTableFromDelete(%v) = %q, want %q", tt.words, result, tt.expected)
			}
		}
	})
}
