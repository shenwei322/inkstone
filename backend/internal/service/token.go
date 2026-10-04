package service

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

type Claims struct {
	UserID   uint   `json:"uid"`
	Username string `json:"uname"`
	Role     string `json:"role"`
	Type     string `json:"typ"`
	// Ver 是签发时的用户令牌代次（model.User.TokenVersion）。
	// 校验方比对库中当前值，用于「改密码 / 封禁 / 强制下线」后立即作废旧令牌。
	// 用指针区分「旧令牌没有该字段(缺失)」与「版本恰好为 0」：
	// 历史令牌缺失时视为版本 0，与初始 TokenVersion 一致，升级后不会误伤。
	Ver *int64 `json:"ver,omitempty"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type TokenManager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewTokenManager(secret string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// GeneratePair 签发 access/refresh 双令牌。tokenVersion 取自用户当前的
// TokenVersion，校验方可据此判定令牌是否已被撤销。
func (m *TokenManager) GeneratePair(userID uint, username, role string, tokenVersion int64) (*TokenPair, error) {
	access, err := m.generate(userID, username, role, TokenTypeAccess, m.accessTTL, tokenVersion)
	if err != nil {
		return nil, err
	}
	refresh, err := m.generate(userID, username, role, TokenTypeRefresh, m.refreshTTL, tokenVersion)
	if err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(m.accessTTL.Seconds()),
	}, nil
}

func (m *TokenManager) generate(userID uint, username, role, typ string, ttl time.Duration, tokenVersion int64) (string, error) {
	ver := tokenVersion
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		Type:     typ,
		Ver:      &ver,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "blog-platform",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// TokenVersionOf 返回令牌声明的代次；字段缺失（旧令牌）时视为 0。
func (c *Claims) TokenVersionOf() int64 {
	if c == nil || c.Ver == nil {
		return 0
	}
	return *c.Ver
}

func (m *TokenManager) Parse(tokenString, expectedType string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	if claims.Type != expectedType {
		return nil, errors.New("invalid token type")
	}
	return claims, nil
}
