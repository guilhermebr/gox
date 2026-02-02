package supabase

import "strings"

// Config represents the configuration options for connecting to a Supabase database.
type Config struct {
	URL string `conf:"env:SUPABASE_URL,required"`
	Key string `conf:"env:SUPABASE_KEY,required"`
}

// IsConfigured returns true when both URL and Key contain
// non-empty, non-placeholder values (e.g. not "your-url-here").
func (c Config) IsConfigured() bool {
	url := strings.TrimSpace(c.URL)
	key := strings.TrimSpace(c.Key)

	if url == "" || key == "" {
		return false
	}

	placeholders := []string{
		"your-url-here",
		"your-key-here",
		"<url>",
		"<key>",
		"TODO",
	}
	for _, p := range placeholders {
		if strings.EqualFold(url, p) || strings.EqualFold(key, p) {
			return false
		}
	}

	return true
}
