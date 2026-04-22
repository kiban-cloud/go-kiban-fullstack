package jwt

import (
	"fmt"
	"time"

	jwt_domain "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/jwt"

	"github.com/golang-jwt/jwt/v5"
	errorsWrapper "github.com/pkg/errors"
)

type JwtService struct {
	secretKey []byte
}

func NewJwtService(secretKey string) *JwtService {
	return &JwtService{
		secretKey: []byte(secretKey),
	}
}

type TokenClaimsInfra struct {
	jwt.RegisteredClaims
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

func (g *JwtService) GenerateToken(subject string, id string, tenantID string, expiresAt time.Time) (string, error) {
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, TokenClaimsInfra{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   subject,
		},
		ID:       id,
		TenantID: tenantID,
	}).SignedString(g.secretKey)

	if err != nil {
		return "", errorsWrapper.Wrap(err, "error when trying to generate token")
	}

	return token, nil
}

func (g *JwtService) ParseToken(tokenString string) (*jwt_domain.TokenClaims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&TokenClaimsInfra{},
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf(
					"unexpected signing method: %v",
					token.Header["alg"],
				)
			}
			return g.secretKey, nil
		},
	)

	if err != nil {
		return nil, errorsWrapper.Wrap(err, "error when trying to parse token")
	}

	if claims, ok := token.Claims.(*TokenClaimsInfra); ok && token.Valid {
		return &jwt_domain.TokenClaims{
			Subject:  claims.Subject,
			ID:       claims.ID,
			TenantID: claims.TenantID,
		}, nil
	}

	return nil, errorsWrapper.New("error when trying to parse token: invalid token")
}
