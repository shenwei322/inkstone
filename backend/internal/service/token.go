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
	// SessionExpiresIn 是 refresh（会话）令牌的剩余秒数。
	//
	// 前端靠它决定「要不要再续」，也是「记住我」时长的唯一权威来源：
	// 客户端只能**收窄**这个值（不勾选记住我 → 提前作废），不能延长，
	// 延长请求会被 TokenManager 按 maxRefreshTTL 截断。
	SessionExpiresIn int64 `json:"session_expires_in"`
}

type TokenManager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	// maxRefreshTTL 是 refresh 令牌的有效期上限。
	//
	// 「记住我」允许客户端申请比默认更长的会话，但不允许无限长：
	// 客户端传 365 天时实际签发的是这个上限，避免一个 Cookie 长期
	// 有效到用户自己都忘了它还在。
	maxRefreshTTL time.Duration
}

func NewTokenManager(secret string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return NewTokenManagerWithMax(secret, accessTTL, refreshTTL, refreshTTL*4)
}

// NewTokenManagerWithMax 允许显式指定 refresh 的上限（用于「记住我」）。
//
// max <= refreshTTL 时按 refreshTTL 处理，保证上限永远不会被配成比默认值还小
// ——那会让「记住我」比不记住存活更短，属于明显的配置事故。
func NewTokenManagerWithMax(secret string, accessTTL, refreshTTL, max time.Duration) *TokenManager {
	if max < refreshTTL {
		max = refreshTTL
	}
	return &TokenManager{
		secret:        []byte(secret),
		accessTTL:     accessTTL,
		refreshTTL:    refreshTTL,
		maxRefreshTTL: max,
	}
}

// GeneratePair 签发 access/refresh 双令牌（refresh 用默认 TTL）。
func (m *TokenManager) GeneratePair(userID uint, username, role string, tokenVersion int64) (*TokenPair, error) {
	return m.GeneratePairWithTTL(userID, username, role, tokenVersion, 0)
}

// GeneratePairWithTTL 签发双令牌，refreshTTL 为 0 时使用默认值。
//
// 超出上限的请求被**静默截断**而不是报错：用户点「记住我一年」时最好的
// 体验是拿到 90 天会话，而不是弹一个「你的要求不被允许」的错误。
func (m *TokenManager) GeneratePairWithTTL(userID uint, username, role string, tokenVersion int64, refreshTTL time.Duration) (*TokenPair, error) {
	if refreshTTL <= 0 {
		refreshTTL = m.refreshTTL
	}
	if m.maxRefreshTTL > 0 && refreshTTL > m.maxRefreshTTL {
		refreshTTL = m.maxRefreshTTL
	}
	now := time.Now()
	access, err := m.generate(userID, username, role, TokenTypeAccess, m.accessTTL, tokenVersion, now)
	if err != nil {
		return nil, err
	}
	refresh, err := m.generate(userID, username, role, TokenTypeRefresh, refreshTTL, tokenVersion, now)
	if err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:      access,
		RefreshToken:     refresh,
		ExpiresIn:        int64(m.accessTTL.Seconds()),
		SessionExpiresIn: int64(refreshTTL.Seconds()),
	}, nil
}

// RefreshTTL 返回默认的 refresh 有效期，供上层计算 Cookie 的 Max-Age。
func (m *TokenManager) RefreshTTL() time.Duration { return m.refreshTTL }

// MaxRefreshTTL 返回 refresh 有效期的上限。
func (m *TokenManager) MaxRefreshTTL() time.Duration { return m.maxRefreshTTL }

func (m *TokenManager) generate(userID uint, username, role, typ string, ttl time.Duration, tokenVersion int64, now time.Time) (string, error) {
	ver := tokenVersion
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		Type:     typ,
		Ver:      &ver,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
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

// TTLOf 返回令牌的剩余有效期（基于 exp 声明）。无 exp 时返回 0。
func (c *Claims) TTLOf(now time.Time) time.Duration {
	if c == nil || c.ExpiresAt == nil {
		return 0
	}
	d := c.ExpiresAt.Time.Sub(now)
	if d < 0 {
		return 0
	}
	return d
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
