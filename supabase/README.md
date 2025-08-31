# supabase

Small helper to construct a Supabase client from env vars.

## Install
```bash
go get github.com/guilhermebr/gox/supabase
```

## Usage
```go
import (
  "github.com/guilhermebr/gox/supabase"
)

client, err := supabase.New("APP")
if err != nil { panic(err) }

// client.From("users").Select("*")
```

## Configuration
- `<PREFIX>_SUPABASE_URL` (required)
- `<PREFIX>_SUPABASE_KEY` (required)


