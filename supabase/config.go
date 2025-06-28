package supabase

// Config represents the configuration options for connecting to a Supabase database.
type Config struct {
	URL string `conf:"env:SUPABASE_URL,required"`
	Key string `conf:"env:SUPABASE_KEY,required"`
}
