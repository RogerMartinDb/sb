// Package identity implements the Identity / Auth service: JWT issuance,
// session management, and credential verification.
package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

// ErrEmailTaken is returned when the email is already registered.
var ErrEmailTaken = errors.New("email already registered")

const (
	sessionTTL = 15 * time.Minute
	jwtTTL     = 1 * time.Hour
)

// Claims holds the JWT payload.
type Claims struct {
	jwt.RegisteredClaims
	UserID    string `json:"sub"`
	KYCStatus string `json:"kyc_status"`
	SessionID string `json:"sid"`
}

// Service handles authentication and session management.
type Service struct {
	db           *pgxpool.Pool
	redisSession *redis.Client // session cache (allkeys-lru)
	jwtSecret    []byte
	logger       *slog.Logger
}

func NewService(db *pgxpool.Pool, redisSession *redis.Client, jwtSecret []byte, logger *slog.Logger) *Service {
	return &Service{
		db:           db,
		redisSession: redisSession,
		jwtSecret:    jwtSecret,
		logger:       logger,
	}
}

// LoginRequest carries credentials from the HTTP layer.
type LoginRequest struct {
	Email    string
	Password string
}

// LoginResponse contains the JWT and a session token.
type LoginResponse struct {
	AccessToken string
	ExpiresAt   time.Time
}

// RegisterRequest carries new-account details from the HTTP layer.
type RegisterRequest struct {
	Email    string
	Password string
}

// Register creates a new user account and returns a JWT, the same as Login.
// Returns ErrEmailTaken if the email is already in use.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*LoginResponse, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	var userID, kycStatus string
	err = s.db.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 RETURNING id, kyc_status`,
		req.Email, string(hash),
	).Scan(&userID, &kycStatus)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}

	sessionID, err := randomHex(16)
	if err != nil {
		return nil, fmt.Errorf("generate session id: %w", err)
	}

	expiresAt := time.Now().Add(jwtTTL)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "sportsbook",
		},
		UserID:    userID,
		KYCStatus: kycStatus,
		SessionID: sessionID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("sign jwt: %w", err)
	}

	sessionKey := fmt.Sprintf("session:%s", sessionID)
	sessionVal := fmt.Sprintf("%s|%s", userID, kycStatus)
	if err := s.redisSession.Set(ctx, sessionKey, sessionVal, sessionTTL).Err(); err != nil {
		return nil, fmt.Errorf("store session: %w", err)
	}

	s.logger.Info("identity: register success", "user_id", userID)
	return &LoginResponse{AccessToken: signed, ExpiresAt: expiresAt}, nil
}

// Login verifies credentials and issues a JWT + Redis session.
func (s *Service) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	var (
		userID       string
		passwordHash string
		kycStatus    string
	)
	err := s.db.QueryRow(ctx,
		`SELECT id, password_hash, kyc_status FROM users WHERE email = $1`,
		req.Email,
	).Scan(&userID, &passwordHash, &kycStatus)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	sessionID, err := randomHex(16)
	if err != nil {
		return nil, fmt.Errorf("generate session id: %w", err)
	}

	expiresAt := time.Now().Add(jwtTTL)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "sportsbook",
		},
		UserID:    userID,
		KYCStatus: kycStatus,
		SessionID: sessionID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("sign jwt: %w", err)
	}

	// Store session in Redis for validation by Bet Acceptance.
	// key: session:{sessionID}  value: userID|kycStatus
	sessionKey := fmt.Sprintf("session:%s", sessionID)
	sessionVal := fmt.Sprintf("%s|%s", userID, kycStatus)
	if err := s.redisSession.Set(ctx, sessionKey, sessionVal, sessionTTL).Err(); err != nil {
		return nil, fmt.Errorf("store session: %w", err)
	}

	s.logger.Info("identity: login success", "user_id", userID)
	return &LoginResponse{AccessToken: signed, ExpiresAt: expiresAt}, nil
}

// ValidateToken verifies a JWT and returns the user_id.
// Called by the API Gateway on every request.
func (s *Service) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid claims")
	}
	return claims, nil
}

// RefreshSession slides the TTL on an existing Redis session.
// Call on every authenticated request to implement 15-minute sliding expiry.
func (s *Service) RefreshSession(ctx context.Context, sessionID string) error {
	key := fmt.Sprintf("session:%s", sessionID)
	return s.redisSession.Expire(ctx, key, sessionTTL).Err()
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
