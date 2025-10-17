package am_token

import (
	"crypto/rsa"
	"errors"
	"time"

	"angrymiao-ai-server/src/configs"

	"os"

	"github.com/golang-jwt/jwt/v4"
)

var (
	verifyKey *rsa.PublicKey

	issue string
)

type JWTClaims struct {
	jwt.StandardClaims

	UserID   int    `json:"user_id"`
	DeviceID string `json:"device_id"`
	Role     string `json:"role"`
}

func Init(c *configs.Config) error {

	issue = c.Casbin.JWT.Issuer

	verifyBytes, err := os.ReadFile(c.Casbin.JWT.PublicKeyPath)
	if err != nil {
		return err
	}

	verifyKey, err = jwt.ParseRSAPublicKeyFromPEM(verifyBytes)
	if err != nil {
		return err
	}

	return nil
}

// ParseToken 解析JWT 校验am token
func ParseToken(tokenString string) (*JWTClaims, error) {
	// 解析token
	token, err := jwt.ParseWithClaims(
		tokenString,
		&JWTClaims{},
		func(token *jwt.Token) (i interface{}, err error) {
			return verifyKey, nil
		})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		if isExpire(claims.ExpiresAt) {
			return nil, errors.New("token expired")
		}

		if claims.Issuer != issue {
			return nil, errors.New("token issuer not match")
		}

		return claims, nil
	}

	return nil, err
}

func isExpire(expire int64) bool {
	return expire-time.Now().Unix() < 0
}
