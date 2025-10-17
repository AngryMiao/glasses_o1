package auth

import (
    "errors"
    "fmt"
    "time"

    "github.com/golang-jwt/jwt/v5"
)

type AuthToken struct {
    secretKey []byte
}

func NewAuthToken(secretKey string) *AuthToken {
    if secretKey == "" {
        fmt.Println("Error! secret key cannot be empty")
    }
    return &AuthToken{secretKey: []byte(secretKey)}
}

// GenerateToken 生成JWT token，默认1小时有效期
func (at *AuthToken) GenerateToken(deviceID string) (string, error) {
    return at.GenerateTokenWithExpiry(0, deviceID, time.Hour)
}

// GenerateTokenWithExpiry 生成指定有效期的JWT token
func (at *AuthToken) GenerateTokenWithExpiry(userID uint, deviceID string, expiry time.Duration) (string, error) {
    expireTime := time.Now().Add(expiry)
    claims := jwt.MapClaims{
        "user_id":   userID,
        "device_id": deviceID,
        "exp":       expireTime.Unix(),
        "iat":       time.Now().Unix(),
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    tokenString, err := token.SignedString(at.secretKey)
    if err != nil {
        return "", fmt.Errorf("failed to sign token: %w", err)
    }
    return tokenString, nil
}

// VerifyToken 校验设备token
func (at *AuthToken) VerifyToken(tokenString string, ignoreExpiry ...bool) (bool, string, uint, error) {
    if at == nil {
        return false, "", 0, errors.New("AuthToken instance is nil")
    }
    if at.secretKey == nil {
        return false, "", 0, errors.New("secret key is not initialized")
    }
    skipExpiry := false
    if len(ignoreExpiry) > 0 {
        skipExpiry = ignoreExpiry[0]
    }
    parser := jwt.NewParser()
    if skipExpiry {
        parser = jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
    }
    token, err := parser.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
        if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
        }
        return at.secretKey, nil
    })
    if err != nil && !skipExpiry {
        return false, "", 0, fmt.Errorf("failed to parse token: %w", err)
    }
    if skipExpiry {
        if token == nil {
            return false, "", 0, errors.New("failed to parse token")
        }
    } else {
        if !token.Valid {
            return false, "", 0, errors.New("invalid token")
        }
    }
    claims, ok := token.Claims.(jwt.MapClaims)
    if !ok {
        return false, "", 0, errors.New("invalid claims")
    }
    deviceID, ok := claims["device_id"].(string)
    if !ok {
        return false, "", 0, errors.New("invalid device_id in claims")
    }
    userID, ok := claims["user_id"].(float64)
    if !ok {
        return false, "", 0, errors.New("invalid user_id in claims")
    }
    return true, deviceID, uint(userID), nil
}