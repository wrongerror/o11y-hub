package auth

import (
	"fmt"
	"time"

	"github.com/lestrrat-go/jwx/jwa"
	"github.com/lestrrat-go/jwx/jwt"
)

// PixieJWTClaims represents the JWT claims needed for Pixie authentication
type PixieJWTClaims struct {
	UserID    string   `json:"UserID"`
	OrgID     string   `json:"OrgID"`
	Email     string   `json:"Email"`
	Scopes    []string `json:"Scopes"`
	ExpiresAt int64    `json:"exp"`
	IssuedAt  int64    `json:"iat"`
	Audience  string   `json:"aud"`
}

// GenerateJWTToken generates a JWT token for Pixie authentication
func GenerateJWTToken(signingKey string, userID, orgID, email string, duration time.Duration) (string, error) {
	now := time.Now()
	expiresAt := now.Add(duration)

	// Create JWT token
	token := jwt.New()

	// Set standard claims with correct audience
	if err := token.Set(jwt.ExpirationKey, expiresAt.Unix()); err != nil {
		return "", fmt.Errorf("failed to set expiration: %w", err)
	}
	if err := token.Set(jwt.IssuedAtKey, now.Unix()); err != nil {
		return "", fmt.Errorf("failed to set issued at: %w", err)
	}
	if err := token.Set(jwt.AudienceKey, "vizier"); err != nil {
		return "", fmt.Errorf("failed to set audience: %w", err)
	}

	// Set custom claims
	if err := token.Set("UserID", userID); err != nil {
		return "", fmt.Errorf("failed to set UserID: %w", err)
	}
	if err := token.Set("OrgID", orgID); err != nil {
		return "", fmt.Errorf("failed to set OrgID: %w", err)
	}
	if err := token.Set("Email", email); err != nil {
		return "", fmt.Errorf("failed to set Email: %w", err)
	}
	if err := token.Set("Scopes", "user"); err != nil {
		return "", fmt.Errorf("failed to set Scopes: %w", err)
	}

	// Sign the token
	key := []byte(signingKey)
	signed, err := jwt.Sign(token, jwa.HS256, key)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return string(signed), nil
}

// GenerateServiceJWTToken generates a JWT token for service authentication
func GenerateServiceJWTToken(signingKey string, serviceName string, duration time.Duration) (string, error) {
	now := time.Now()
	expiresAt := now.Add(duration)

	// Create JWT token
	token := jwt.New()

	// Set standard claims with correct audience
	if err := token.Set(jwt.ExpirationKey, expiresAt.Unix()); err != nil {
		return "", fmt.Errorf("failed to set expiration: %w", err)
	}
	if err := token.Set(jwt.IssuedAtKey, now.Unix()); err != nil {
		return "", fmt.Errorf("failed to set issued at: %w", err)
	}
	if err := token.Set(jwt.AudienceKey, "vizier"); err != nil {
		return "", fmt.Errorf("failed to set audience: %w", err)
	}

	// Set service claims
	if err := token.Set("ServiceID", serviceName); err != nil {
		return "", fmt.Errorf("failed to set ServiceID: %w", err)
	}
	if err := token.Set("Scopes", "service"); err != nil {
		return "", fmt.Errorf("failed to set Scopes: %w", err)
	}

	// Sign the token
	key := []byte(signingKey)
	signed, err := jwt.Sign(token, jwa.HS256, key)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return string(signed), nil
}
