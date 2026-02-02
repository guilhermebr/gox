package supabase

import (
	"time"

	"github.com/supabase-community/gotrue-go/types"
)

// UserInfo is a domain-agnostic representation of a Supabase Auth user.
type UserInfo struct {
	ID             string
	Email          string
	Phone          string
	EmailConfirmed bool
	PhoneConfirmed bool
	Role           string
	UserMetadata   map[string]interface{}
	AppMetadata    map[string]interface{}
	LastSignInAt   *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// userInfoFromSDK converts a gotrue-go types.User to a UserInfo.
// Returns nil when u is nil.
func userInfoFromSDK(u *types.User) *UserInfo {
	if u == nil {
		return nil
	}

	info := &UserInfo{
		ID:           u.ID.String(),
		Email:        u.Email,
		Phone:        u.Phone,
		Role:         u.Role,
		UserMetadata: u.UserMetadata,
		AppMetadata:  u.AppMetadata,
		LastSignInAt: u.LastSignInAt,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
	}

	info.EmailConfirmed = u.EmailConfirmedAt != nil
	info.PhoneConfirmed = u.PhoneConfirmedAt != nil

	return info
}
