package api

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"meshserver/internal/config"
	"meshserver/internal/repository"
)

// AccessTokenClaims is the JWT payload for HTTP API access (after libp2p-style challenge verify).
type AccessTokenClaims struct {
	UserDBID  uint64 `json:"uid"`
	UserExtID string `json:"user_id"`
	PeerID    string `json:"peer_id"`
	jwt.RegisteredClaims
}

// ResolveHTTPJWTSecret returns the HMAC key for signing access tokens.
// If MESHSERVER_HTTP_JWT_SECRET (config.HTTPJWTSecret) is set, it is used; otherwise SHA-256 of the node key file is used.
func ResolveHTTPJWTSecret(cfg *config.Config) ([]byte, error) {
	if s := strings.TrimSpace(cfg.HTTPJWTSecret); s != "" {
		return []byte(s), nil
	}
	raw, err := os.ReadFile(cfg.NodeKeyPath)
	if err != nil {
		return nil, fmt.Errorf("http jwt secret: set MESHSERVER_HTTP_JWT_SECRET or readable MESHSERVER_NODE_KEY_PATH: %w", err)
	}
	sum := sha256.Sum256(raw)
	out := make([]byte, len(sum))
	copy(out, sum[:])
	return out, nil
}

// SignHTTPAccessToken issues a signed JWT for the authenticated user.
func SignHTTPAccessToken(secret []byte, user *repository.User, ttl time.Duration) (token string, expiresAt time.Time, err error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	now := time.Now().UTC()
	expiresAt = now.Add(ttl)
	claims := AccessTokenClaims{
		UserDBID:  user.ID,
		UserExtID: user.UserID,
		PeerID:    user.PeerID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.UserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, err = t.SignedString(secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// ParseHTTPAccessToken validates a Bearer JWT and returns claims.
func ParseHTTPAccessToken(secret []byte, tokenString string) (*AccessTokenClaims, error) {
	claims := &AccessTokenClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

// ParseBearerAuthorization returns the token string from Authorization: Bearer <token>.
func ParseBearerAuthorization(r *http.Request) (string, error) {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if h == "" {
		return "", fmt.Errorf("missing authorization header")
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", fmt.Errorf("expected Bearer token")
	}
	tok := strings.TrimSpace(parts[1])
	if tok == "" {
		return "", fmt.Errorf("empty bearer token")
	}
	return tok, nil
}

// ClaimsFromHTTPRequest parses Authorization: Bearer and validates the JWT.
func ClaimsFromHTTPRequest(r *http.Request, secret []byte) (*AccessTokenClaims, error) {
	raw, err := ParseBearerAuthorization(r)
	if err != nil {
		return nil, err
	}
	return ParseHTTPAccessToken(secret, raw)
}
