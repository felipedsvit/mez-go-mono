package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/auth/lockout"
)

type LoginInput struct {
	Email     string
	Password  string
	IP        string
	UserAgent string
}

type LoginResult struct {
	SessionID admin.SessionID
	User      admin.AdminUser
}

type LoginUseCase interface {
	LoginLocal(ctx context.Context, input LoginInput) (LoginResult, error)
	LoginOIDC(ctx context.Context, code, codeVerifier, ip, userAgent string) (LoginResult, error)
	StartOIDC(ctx context.Context, redirectAfter string) (authURL string, state string, err error)
}

type LoginService struct {
	users       admin.UserRepo
	roles       admin.RoleRepo
	sessions    admin.SessionStore
	audit       admin.AuditRepo
	hasher      admin.PasswordHasher
	idp         admin.IdP
	stateStore  admin.StateStore
	lockout     *lockout.Tracker
	sessionTTL  time.Duration
	oidcEnabled bool
}

func NewLoginService(
	users admin.UserRepo,
	roles admin.RoleRepo,
	sessions admin.SessionStore,
	audit admin.AuditRepo,
	hasher admin.PasswordHasher,
	idp admin.IdP,
	stateStore admin.StateStore,
	tracker *lockout.Tracker,
	sessionTTL time.Duration,
	oidcEnabled bool,
) *LoginService {
	if tracker == nil {
		// Default: 5 fails / 15min. Pass lockout.New(0, 0, nil) to disable.
		tracker = lockout.New(5, 15*time.Minute, nil)
	}
	return &LoginService{
		users:       users,
		roles:       roles,
		sessions:    sessions,
		audit:       audit,
		hasher:      hasher,
		idp:         idp,
		stateStore:  stateStore,
		lockout:     tracker,
		sessionTTL:  sessionTTL,
		oidcEnabled: oidcEnabled,
	}
}

func (s *LoginService) LoginLocal(ctx context.Context, input LoginInput) (LoginResult, error) {
	if input.Email == "" || input.Password == "" {
		return LoginResult{}, admin.ErrInvalidCredentials
	}

	email := admin.NormalizeEmail(input.Email)
	if !admin.ValidEmail(email) {
		return LoginResult{}, admin.ErrInvalidCredentials
	}

	// Lockout check happens BEFORE the password lookup to avoid using it
	// as a side-channel for valid email enumeration. The failed attempt
	// is recorded but the password store is not consulted.
	if s.lockout.IsLockedOut(email) {
		s.recordAudit(ctx, "", email, admin.ActionAuthLoginFailure, input.IP, input.UserAgent)
		return LoginResult{}, admin.ErrTooManyAttempts
	}

	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if s.lockout.RecordFailure(email) {
			// Just crossed the threshold — return lockout rather than
			// invalid credentials so the client knows the gate has closed.
			s.recordAudit(ctx, "", email, admin.ActionAuthLoginFailure, input.IP, input.UserAgent)
			return LoginResult{}, admin.ErrTooManyAttempts
		}
		s.recordAudit(ctx, "", email, admin.ActionAuthLoginFailure, input.IP, input.UserAgent)
		return LoginResult{}, admin.ErrInvalidCredentials
	}

	if !user.IsLocal() {
		s.recordAudit(ctx, user.ID, email, admin.ActionAuthLoginFailure, input.IP, input.UserAgent)
		return LoginResult{}, admin.ErrInvalidCredentials
	}

	valid, err := s.hasher.Verify(ctx, user.PasswordHash, input.Password)
	if err != nil || !valid {
		if s.lockout.RecordFailure(email) {
			s.recordAudit(ctx, user.ID, email, admin.ActionAuthLoginFailure, input.IP, input.UserAgent)
			return LoginResult{}, admin.ErrTooManyAttempts
		}
		s.recordAudit(ctx, user.ID, email, admin.ActionAuthLoginFailure, input.IP, input.UserAgent)
		return LoginResult{}, admin.ErrInvalidCredentials
	}

	if !user.IsActive() {
		s.recordAudit(ctx, user.ID, email, admin.ActionAuthLoginFailure, input.IP, input.UserAgent)
		return LoginResult{}, admin.ErrUserDisabled
	}

	s.lockout.RecordSuccess(email)
	return s.completeLogin(ctx, user, input.IP, input.UserAgent)
}

func (s *LoginService) LoginOIDC(ctx context.Context, code, codeVerifier, ip, userAgent string) (LoginResult, error) {
	if !s.oidcEnabled {
		return LoginResult{}, admin.ErrInvalidCredentials
	}

	rawIDToken, err := s.idp.Exchange(ctx, code, codeVerifier)
	if err != nil {
		return LoginResult{}, admin.ErrInvalidCredentials
	}

	userInfo, err := s.idp.VerifyIDToken(ctx, rawIDToken)
	if err != nil {
		return LoginResult{}, admin.ErrInvalidCredentials
	}

	if !userInfo.EmailVerified {
		return LoginResult{}, admin.ErrEmailNotVerified
	}

	user, err := s.users.GetByIDP(ctx, userInfo.Issuer, userInfo.Subject)
	if err != nil {
		return LoginResult{}, admin.ErrUserNotProvisioned
	}

	if !user.IsActive() {
		s.recordAudit(ctx, user.ID, user.Email, admin.ActionAuthLoginFailure, ip, userAgent)
		return LoginResult{}, admin.ErrUserDisabled
	}

	return s.completeLogin(ctx, user, ip, userAgent)
}

func (s *LoginService) StartOIDC(ctx context.Context, redirectAfter string) (string, string, error) {
	if !s.oidcEnabled {
		return "", "", admin.ErrInvalidCredentials
	}

	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", "", err
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	challenge := oidcChallenge(verifier)

	oidcState := admin.OIDCState{
		State:         state,
		CodeVerifier:  verifier,
		RedirectAfter: redirectAfter,
	}
	if err := s.stateStore.SaveState(ctx, state, oidcState, 300); err != nil {
		return "", "", err
	}

	authURL := s.idp.AuthCodeURL(state, challenge)
	return authURL, state, nil
}

func (s *LoginService) completeLogin(ctx context.Context, user admin.AdminUser, ip, userAgent string) (LoginResult, error) {
	if err := s.users.UpdateLastLogin(ctx, user.ID); err != nil {
		return LoginResult{}, err
	}

	sessionID := make([]byte, 32)
	if _, err := rand.Read(sessionID); err != nil {
		return LoginResult{}, err
	}

	session := admin.Session{
		ID:        admin.SessionID(base64.RawURLEncoding.EncodeToString(sessionID)),
		UserID:    user.ID,
		Email:     user.Email,
		ExpiresAt: time.Now().Add(s.sessionTTL),
		CreatedAt: time.Now(),
	}

	if err := s.sessions.Save(ctx, session, s.sessionTTL); err != nil {
		return LoginResult{}, err
	}

	s.recordAudit(ctx, user.ID, user.Email, admin.ActionAuthLoginSuccess, ip, userAgent)

	return LoginResult{SessionID: session.ID, User: user}, nil
}

func (s *LoginService) recordAudit(ctx context.Context, actorID admin.AdminUserID, email string, action admin.Action, ip, userAgent string) {
	entry := &admin.AuditEntry{
		ActorID:   actorID,
		Action:    action,
		IP:        ip,
		UserAgent: userAgent,
	}
	_ = s.audit.Record(ctx, entry)
}

func oidcChallenge(verifier string) string {
	return admin.ChallengeFromVerifier(verifier)
}
