package admin

import (
	"context"
	"crypto/rand"
	"encoding/base64"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type UserUseCase interface {
	List(ctx context.Context, filter admin.UserFilter) ([]admin.AdminUser, error)
	Get(ctx context.Context, id admin.AdminUserID) (admin.AdminUser, error)
	Invite(ctx context.Context, email, name string, roleID admin.RoleID, actor Actor) (*admin.AdminUser, string, error)
	SetStatus(ctx context.Context, id admin.AdminUserID, status admin.UserStatus, actor Actor) error
	AssignRole(ctx context.Context, userID admin.AdminUserID, roleID admin.RoleID, tenantID string, actor Actor) error
	RevokeRole(ctx context.Context, userID admin.AdminUserID, roleID admin.RoleID, tenantID string, actor Actor) error
}

type UserService struct {
	users  admin.UserRepo
	roles  admin.RoleRepo
	audit  admin.AuditRepo
	hasher admin.PasswordHasher
}

func NewUserService(users admin.UserRepo, roles admin.RoleRepo, audit admin.AuditRepo, hasher admin.PasswordHasher) *UserService {
	return &UserService{
		users:  users,
		roles:  roles,
		audit:  audit,
		hasher: hasher,
	}
}

func (s *UserService) List(ctx context.Context, filter admin.UserFilter) ([]admin.AdminUser, error) {
	return s.users.List(ctx, filter)
}

func (s *UserService) Get(ctx context.Context, id admin.AdminUserID) (admin.AdminUser, error) {
	return s.users.GetByID(ctx, id)
}

func (s *UserService) Invite(ctx context.Context, email, name string, roleID admin.RoleID, actor Actor) (*admin.AdminUser, string, error) {
	email = admin.NormalizeEmail(email)
	if !admin.ValidEmail(email) {
		return nil, "", admin.ErrInvalidEmail
	}

	passBytes := make([]byte, 24)
	if _, err := rand.Read(passBytes); err != nil {
		return nil, "", err
	}
	tempPassword := base64.RawURLEncoding.EncodeToString(passBytes)

	hash, err := s.hasher.Hash(ctx, tempPassword)
	if err != nil {
		return nil, "", err
	}

	user := &admin.AdminUser{
		Email:        email,
		Name:         name,
		AuthKind:     admin.AuthKindLocal,
		Status:       admin.UserStatusInvited,
		PasswordHash: hash,
	}

	if err := s.users.Insert(ctx, user); err != nil {
		return nil, "", err
	}

	if roleID != "" {
		_ = s.users.AssignRole(ctx, user.ID, roleID, "")
	}

	s.recordAudit(ctx, actor, admin.ActionUserCreate, string(user.ID), "")
	return user, tempPassword, nil
}

func (s *UserService) SetStatus(ctx context.Context, id admin.AdminUserID, status admin.UserStatus, actor Actor) error {
	if id == actor.ID {
		return admin.ErrSelfLockout
	}

	if err := s.users.SetStatus(ctx, id, status); err != nil {
		return err
	}

	s.recordAudit(ctx, actor, admin.ActionUserStatus, string(id), "")
	return nil
}

func (s *UserService) AssignRole(ctx context.Context, userID admin.AdminUserID, roleID admin.RoleID, tenantID string, actor Actor) error {
	if err := s.users.AssignRole(ctx, userID, roleID, tenantID); err != nil {
		return err
	}

	s.recordAudit(ctx, actor, admin.ActionUserRoleAssign, string(userID), tenantID)
	return nil
}

func (s *UserService) RevokeRole(ctx context.Context, userID admin.AdminUserID, roleID admin.RoleID, tenantID string, actor Actor) error {
	if err := s.users.RevokeRole(ctx, userID, roleID, tenantID); err != nil {
		return err
	}

	s.recordAudit(ctx, actor, admin.ActionUserRoleRevoke, string(userID), tenantID)
	return nil
}

func (s *UserService) recordAudit(ctx context.Context, actor Actor, action admin.Action, targetID, tenantID string) {
	entry := &admin.AuditEntry{
		ActorID:  actor.ID,
		Action:   action,
		TargetID: targetID,
		TenantID: tenantID,
		IP:       actor.IP,
	}
	_ = s.audit.Record(ctx, entry)
}
