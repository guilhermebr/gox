package supabase

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/supabase-community/gotrue-go/types"
	supabase "github.com/supabase-community/supabase-go"
)

var ErrNilClient = errors.New("supabase client is nil")

// SignUpWithEmail registers a new user with email and password.
// metadata is optional user metadata attached to the account.
// Returns the new user's ID.
func SignUpWithEmail(_ context.Context, client *supabase.Client, email, password string, metadata map[string]interface{}) (string, error) {
	if client == nil {
		return "", ErrNilClient
	}

	resp, err := client.Auth.Signup(types.SignupRequest{
		Email:    email,
		Password: password,
		Data:     metadata,
	})
	if err != nil {
		return "", fmt.Errorf("supabase signup: %w", err)
	}

	return resp.ID.String(), nil
}

// SignInWithEmail authenticates a user with email and password.
// Returns the access token from the resulting session.
func SignInWithEmail(_ context.Context, client *supabase.Client, email, password string) (string, error) {
	if client == nil {
		return "", ErrNilClient
	}

	session, err := client.SignInWithEmailPassword(email, password)
	if err != nil {
		return "", fmt.Errorf("supabase sign-in: %w", err)
	}

	return session.AccessToken, nil
}

// GetUserFromToken retrieves user information for the given access token.
// It updates the client's auth session before fetching the user.
func GetUserFromToken(_ context.Context, client *supabase.Client, token string) (*UserInfo, error) {
	if client == nil {
		return nil, ErrNilClient
	}

	client.UpdateAuthSession(types.Session{AccessToken: token})

	resp, err := client.Auth.GetUser()
	if err != nil {
		return nil, fmt.Errorf("supabase get user: %w", err)
	}

	return userInfoFromSDK(&resp.User), nil
}

// AdminDeleteUser deletes a user by ID. Requires a client initialised with a
// service-role key.
func AdminDeleteUser(_ context.Context, client *supabase.Client, userID string) error {
	if client == nil {
		return ErrNilClient
	}

	uid, err := uuid.Parse(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID %q: %w", userID, err)
	}

	if err := client.Auth.AdminDeleteUser(types.AdminDeleteUserRequest{
		UserID: uid,
	}); err != nil {
		return fmt.Errorf("supabase admin delete user: %w", err)
	}

	return nil
}
