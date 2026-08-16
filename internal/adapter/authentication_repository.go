package adapter

import (
	"context"
	"errors"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
)

// AuthenticationRepository 将用户密码与会话数据库操作适配为账号认证应用端口。
type AuthenticationRepository struct {
	// store 保存数据库聚合入口；用户密码只在适配器调用期间参与校验或写入。
	store *db.Store
}

// NewAuthenticationRepository 构造认证数据库适配器。
func NewAuthenticationRepository(store *db.Store) *AuthenticationRepository {
	return &AuthenticationRepository{store: store}
}

// IsSystemInitialized 查询数据库中是否已经存在管理员账号。
func (r *AuthenticationRepository) IsSystemInitialized(ctx context.Context) (bool, error) {
	// validationErr 表示认证用户仓储是否已经完成装配。
	validationErr := r.validateUsers()
	if validationErr != nil {
		return false, validationErr
	}
	return r.store.Users.IsSystemInitialized(ctx)
}

// InitializeAdmin 创建或重置管理员账号，保持命令行初始化入口的兼容语义。
func (r *AuthenticationRepository) InitializeAdmin(ctx context.Context, email, password string) (bool, error) {
	// validationErr 表示认证用户仓储是否已经完成装配。
	validationErr := r.validateUsers()
	if validationErr != nil {
		return false, validationErr
	}
	// existing、lookupErr 保存现有管理员和查询错误；初始化调用方会在外层先做未初始化检查。
	existing, lookupErr := r.store.Users.GetAdmin(ctx)
	if lookupErr != nil && !errors.Is(lookupErr, db.ErrNotFound) {
		return false, lookupErr
	}
	if existing != nil {
		// updated、updateErr 保存既有管理员密码更新结果。
		updated, updateErr := r.store.Users.UpdatePassword(ctx, existing.Username, password)
		if updateErr != nil {
			return false, updateErr
		}
		if !updated {
			return false, errors.New("管理员存在但密码未更新")
		}
		return false, r.store.Users.SetAdmin(ctx, existing.Username)
	}
	// created、createErr 保存新管理员创建结果。
	created, createErr := r.store.Users.Create(ctx, "admin", email, password)
	if createErr != nil {
		return false, createErr
	}
	if !created {
		return false, errors.New("创建管理员失败：用户名或邮箱可能已存在")
	}
	return true, r.store.Users.SetAdmin(ctx, "admin")
}

// UsernameByEmail 按邮箱取得登录用户名，不向应用层暴露密码哈希等完整用户字段。
func (r *AuthenticationRepository) UsernameByEmail(ctx context.Context, email string) (string, error) {
	// validationErr 表示认证用户仓储是否已经完成装配。
	validationErr := r.validateUsers()
	if validationErr != nil {
		return "", validationErr
	}
	// user、lookupErr 保存邮箱查询结果；仅提取登录用户名后立即丢弃数据库模型。
	user, lookupErr := r.store.Users.GetByEmail(ctx, email)
	if lookupErr != nil {
		return "", lookupErr
	}
	if user == nil {
		return "", db.ErrNotFound
	}
	return user.Username, nil
}

// VerifyPassword 校验密码并转换为不含敏感字段的应用身份。
func (r *AuthenticationRepository) VerifyPassword(ctx context.Context, username, password string) (accountapp.AuthUser, bool, error) {
	// validationErr 表示认证用户仓储是否已经完成装配。
	validationErr := r.validateUsers()
	if validationErr != nil {
		return accountapp.AuthUser{}, false, validationErr
	}
	// user、matched、verifyErr 保存数据库密码校验结果；密码哈希不跨出本方法。
	user, matched, verifyErr := r.store.Users.VerifyAndUpgrade(ctx, username, password)
	if verifyErr != nil {
		if errors.Is(verifyErr, db.ErrPasswordMismatch) {
			return accountapp.AuthUser{}, false, accountapp.ErrPasswordMismatch
		}
		return accountapp.AuthUser{}, false, verifyErr
	}
	if !matched || user == nil {
		return accountapp.AuthUser{}, false, nil
	}
	return accountapp.AuthUser{ID: user.ID, Username: user.Username, IsAdmin: user.IsAdmin}, true, nil
}

// UpdatePassword 更新密码并撤销该用户全部旧会话。
func (r *AuthenticationRepository) UpdatePassword(ctx context.Context, username, password string) (bool, error) {
	// validationErr 表示认证用户仓储是否已经完成装配。
	validationErr := r.validateUsers()
	if validationErr != nil {
		return false, validationErr
	}
	return r.store.Users.UpdatePassword(ctx, username, password)
}

// UpdateCredentials 原子更新登录用户名及可选密码，并转换用户名冲突错误。
func (r *AuthenticationRepository) UpdateCredentials(ctx context.Context, userID int64, username, password string) error {
	// validationErr 表示认证用户仓储是否已经完成装配。
	validationErr := r.validateUsers()
	if validationErr != nil {
		return validationErr
	}
	// updateErr 保存登录凭据写入及事务提交结果。
	updateErr := r.store.Users.UpdateCredentials(ctx, userID, username, password)
	if updateErr != nil {
		if errors.Is(updateErr, db.ErrUsernameTaken) {
			return accountapp.ErrUsernameTaken
		}
		return updateErr
	}
	return nil
}

// CreateSession 为认证成功的应用身份创建数据库会话。
func (r *AuthenticationRepository) CreateSession(ctx context.Context, user accountapp.AuthUser) (string, error) {
	if r == nil || r.store == nil || r.store.Sessions == nil {
		return "", errors.New("认证会话数据库适配器未初始化")
	}
	// dbUser 是仅供会话仓储写入的非敏感数据库用户投影。
	dbUser := &db.User{ID: user.ID, Username: user.Username, IsAdmin: user.IsAdmin}
	return r.store.Sessions.Create(ctx, dbUser)
}

// validateUsers 检查认证适配器是否已装配用户仓储。
func (r *AuthenticationRepository) validateUsers() error {
	if r == nil || r.store == nil || r.store.Users == nil {
		return errors.New("认证用户数据库适配器未初始化")
	}
	return nil
}

var _ accountapp.AuthenticationRepository = (*AuthenticationRepository)(nil)
