package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenKind identifies the type of JWT token.
type TokenKind string

const (
	// KindCLI is a CLI user token.
	KindCLI TokenKind = "cli"
	// KindAgent is an agent node token.
	KindAgent TokenKind = "agent"
)

// Claims holds the data embedded in a JWT.
type Claims struct {
	UserID  string    `json:"uid,omitempty"`
	NodeID  string    `json:"nid,omitempty"`
	NodeIDs []string  `json:"nids,omitempty"`
	Kind    TokenKind `json:"kind"`
	jwt.RegisteredClaims
}

// SignCLIToken creates a signed JWT for a CLI client that can access the given
// nodes. It returns the token string, the JTI (JWT ID) used as the unique
// token identifier, and any error. Pass expiry=0 for a non-expiring token.
func SignCLIToken(secret, userID string, nodeIDs []string, expiry time.Duration) (string, string, error) {
	now := time.Now()
	jti := uuid.NewString()
	rc := jwt.RegisteredClaims{
		ID:       jti,
		IssuedAt: jwt.NewNumericDate(now),
	}
	if expiry != 0 {
		rc.ExpiresAt = jwt.NewNumericDate(now.Add(expiry))
	}
	// expiry == 0 means no expiration (token valid until revoked).
	claims := Claims{
		UserID:  userID,
		NodeIDs: nodeIDs,
		Kind:    KindCLI,
		RegisteredClaims: rc,
	}
	tok, err := signToken(secret, claims)
	return tok, jti, err
}

// SignAgentToken creates a signed JWT for an agent node. It returns the token
// string, the JTI used as the unique token identifier, and any error.
func SignAgentToken(secret, nodeID string, expiry time.Duration) (string, string, error) {
	now := time.Now()
	jti := uuid.NewString()
	rc := jwt.RegisteredClaims{
		ID:       jti,
		IssuedAt: jwt.NewNumericDate(now),
	}
	if expiry != 0 {
		rc.ExpiresAt = jwt.NewNumericDate(now.Add(expiry))
	}
	// expiry == 0 means no expiration (token valid until revoked).
	claims := Claims{
		NodeID:           nodeID,
		Kind:             KindAgent,
		RegisteredClaims: rc,
	}
	tok, err := signToken(secret, claims)
	return tok, jti, err
}

func signToken(secret string, claims Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ValidateToken parses and validates a JWT string, returning the embedded Claims.
func ValidateToken(secret, tokenStr string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token claims")
	}
	return claims, nil
}

// HasNodeAccess reports whether the claims grant access to the given nodeID.
// CLI tokens carry a NodeIDs allowlist; agent tokens are bound to a single NodeID.
// A CLI token with "*" in NodeIDs grants access to all nodes.
func HasNodeAccess(claims *Claims, nodeID string) bool {
	if claims == nil {
		return false
	}
	switch claims.Kind {
	case KindAgent:
		return claims.NodeID == nodeID
	case KindCLI:
		for _, id := range claims.NodeIDs {
			if id == "*" || id == nodeID {
				return true
			}
		}
	}
	return false
}
