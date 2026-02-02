# supabase

Helper package to construct a Supabase client from env vars and perform common auth operations.

## Install
```bash
go get github.com/guilhermebr/gox/supabase
```

## Usage

### Client from environment variables
```go
import (
  goxsupa "github.com/guilhermebr/gox/supabase"
)

client, err := goxsupa.New("APP")
if err != nil { panic(err) }
```

### Client from Config struct
```go
cfg := goxsupa.Config{
  URL: "https://your-project.supabase.co",
  Key: "your-anon-key",
}

client, err := goxsupa.NewFromConfig(cfg)
if err != nil { panic(err) }
```

### Check if configured
```go
if cfg.IsConfigured() {
  // URL and Key are non-empty and not placeholder values
}
```

### Auth helpers
```go
ctx := context.Background()

// Sign up
userID, err := goxsupa.SignUpWithEmail(ctx, client, "user@example.com", "password", map[string]interface{}{
  "name": "Jane Doe",
})

// Sign in
token, err := goxsupa.SignInWithEmail(ctx, client, "user@example.com", "password")

// Get user from token
info, err := goxsupa.GetUserFromToken(ctx, client, token)
// info.ID, info.Email, info.EmailConfirmed, ...

// Admin delete (requires service-role key)
err = goxsupa.AdminDeleteUser(ctx, client, userID)
```

## Configuration
- `<PREFIX>_SUPABASE_URL` (required)
- `<PREFIX>_SUPABASE_KEY` (required)
