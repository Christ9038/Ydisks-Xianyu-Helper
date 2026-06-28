package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNotFound 未找到记录。
var ErrNotFound = errors.New("记录不存在")

// ErrUsernameTaken 表示目标用户名已被其他用户占用。
var ErrUsernameTaken = errors.New("用户名已存在")

// Users 用户相关查询。所有方法都直接接受 *sql.DB，由调用方控制事务边界。
type Users struct {
	DB *sql.DB
}

// IsSystemInitialized 系统是否已初始化（至少存在 admin 用户）。
// 对应 Python db_manager.is_system_initialized()。
func (u *Users) IsSystemInitialized(ctx context.Context) (bool, error) {
	var exists int
	err := u.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE is_admin=1)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("检查系统初始化: %w", err)
	}
	return exists == 1, nil
}

// GetAdmin 返回首个管理员账号。
func (u *Users) GetAdmin(ctx context.Context) (*User, error) {
	return u.scanUser(ctx, `SELECT id, username, email, password_hash, is_active, is_admin, created_at, updated_at
		FROM users WHERE is_admin=1 ORDER BY id LIMIT 1`)
}

// Create 创建用户，密码用 bcrypt 哈希。
// 用户名或邮箱重复时返回 false（对应 Python IntegrityError → return False）。
func (u *Users) Create(ctx context.Context, username, email, plainPassword string) (bool, error) {
	hash, err := HashPassword(plainPassword)
	if err != nil {
		return false, fmt.Errorf("哈希密码: %w", err)
	}
	res, err := u.DB.ExecContext(ctx,
		`INSERT INTO users (username, email, password_hash) VALUES (?, ?, ?)`,
		username, email, hash)
	if err != nil {
		// UNIQUE 冲突 → 用户名/邮箱已存在。
		return false, nil
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetByUsername 按用户名查询。
func (u *Users) GetByUsername(ctx context.Context, username string) (*User, error) {
	return u.scanUser(ctx, `SELECT id, username, email, password_hash, is_active, is_admin, created_at, updated_at
		FROM users WHERE username = ?`, username)
}

// GetByEmail 按邮箱查询。
func (u *Users) GetByEmail(ctx context.Context, email string) (*User, error) {
	return u.scanUser(ctx, `SELECT id, username, email, password_hash, is_active, is_admin, created_at, updated_at
		FROM users WHERE email = ?`, email)
}

// GetByID 按 ID 查询。
func (u *Users) GetByID(ctx context.Context, id int64) (*User, error) {
	return u.scanUser(ctx, `SELECT id, username, email, password_hash, is_active, is_admin, created_at, updated_at
		FROM users WHERE id = ?`, id)
}

// VerifyAndUpgrade 验证密码；若命中老 SHA-256 哈希则静默升级到 bcrypt。
// 返回 (user, ok)。ok=false 表示密码错误或用户不存在/未激活。
func (u *Users) VerifyAndUpgrade(ctx context.Context, username, plainPassword string) (*User, bool, error) {
	user, err := u.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if !user.IsActive {
		return nil, false, nil
	}
	matched, needsUpgrade, err := VerifyPassword(user.PasswordHash, plainPassword)
	if err != nil || !matched {
		return nil, false, err
	}
	if needsUpgrade {
		// 静默升级到 bcrypt。
		if hash, e := HashPassword(plainPassword); e == nil {
			_, _ = u.DB.ExecContext(ctx,
				`UPDATE users SET password_hash=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
				hash, user.ID)
			user.PasswordHash = hash
		}
	}
	return user, true, nil
}

// UpdatePassword 更新密码（bcrypt）。返回是否找到用户。
func (u *Users) UpdatePassword(ctx context.Context, username, plainPassword string) (bool, error) {
	hash, err := HashPassword(plainPassword)
	if err != nil {
		return false, fmt.Errorf("哈希密码: %w", err)
	}
	res, err := u.DB.ExecContext(ctx,
		`UPDATE users SET password_hash=?, updated_at=CURRENT_TIMESTAMP WHERE username=?`,
		hash, username)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UpdateCredentials 原子更新用户名及可选密码，并撤销该用户的全部登录会话。
func (u *Users) UpdateCredentials(ctx context.Context, userID int64, username, plainPassword string) error {
	tx, err := u.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE username=? AND id<>?)`, username, userID).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return ErrUsernameTaken
	}

	var res sql.Result
	if plainPassword != "" {
		hash, err := HashPassword(plainPassword)
		if err != nil {
			return fmt.Errorf("哈希密码: %w", err)
		}
		res, err = tx.ExecContext(ctx,
			`UPDATE users SET username=?, password_hash=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			username, hash, userID)
	} else {
		res, err = tx.ExecContext(ctx,
			`UPDATE users SET username=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, username, userID)
	}
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetAdmin 标记用户为管理员（init-admin CLI 用）。
func (u *Users) SetAdmin(ctx context.Context, username string) error {
	_, err := u.DB.ExecContext(ctx, `UPDATE users SET is_admin=1 WHERE username=?`, username)
	return err
}

func (u *Users) scanUser(ctx context.Context, query string, args ...any) (*User, error) {
	var usr User
	var isActive, isAdmin int
	err := u.DB.QueryRowContext(ctx, query, args...).Scan(
		&usr.ID, &usr.Username, &usr.Email, &usr.PasswordHash,
		&isActive, &isAdmin, &usr.CreatedAt, &usr.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	usr.IsActive = isActive != 0
	usr.IsAdmin = isAdmin != 0
	return &usr, nil
}
