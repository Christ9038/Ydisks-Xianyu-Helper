package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound 未找到记录。
var ErrNotFound = errors.New("记录不存在")

// ErrUsernameTaken 表示目标用户名已被其他用户占用。
var ErrUsernameTaken = errors.New("用户名已存在")

// ErrForbidden 表示资源存在但不属于当前用户。
var ErrForbidden = errors.New("无权限操作资源")

// Users 用户相关查询。所有方法都直接接受 *sql.DB，由调用方控制事务边界。
type Users struct {
	DB *sql.DB
}

// IsSystemInitialized 系统是否已初始化（至少存在 admin 用户）。
// IsSystemInitialized 判断系统是否已有管理员账号。
// IsSystemInitialized 负责Is系统Initialized相关处理。
func (u *Users) IsSystemInitialized(ctx context.Context) (bool, error) {
	// exists 保存exists，供当前处理流程使用
	var exists bool
	// err 保存err，供当前处理流程使用
	err := u.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE is_admin=1)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("检查系统初始化: %w", err)
	}
	return exists, nil
}

// GetAdmin 返回首个管理员账号。
func (u *Users) GetAdmin(ctx context.Context) (*User, error) {
	return u.scanUser(ctx, `SELECT id, username, email, password_hash, is_active, is_admin, created_at, updated_at
		FROM users WHERE is_admin=1 ORDER BY id LIMIT 1`)
}

// Create 创建用户，密码用 bcrypt 哈希。
// 用户名或邮箱重复时返回 false。
// Create 创建当前值。
func (u *Users) Create(ctx context.Context, username, email, plainPassword string) (bool, error) {
	// hash、err 保存hash、err，供当前处理流程使用
	hash, err := HashPassword(plainPassword)
	if err != nil {
		return false, fmt.Errorf("哈希密码: %w", err)
	}
	// res、err 保存res、err，供当前处理流程使用
	res, err := u.DB.ExecContext(ctx,
		`INSERT INTO users (username, email, password_hash) VALUES (?, ?, ?)`,
		username, email, hash)
	if err != nil {
		if isUniqueViolation(err) {
			return false, nil
		}
		return false, fmt.Errorf("创建用户: %w", err)
	}
	// n 保存n，供当前处理流程使用
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// isUniqueViolation 负责isUniqueViolation相关处理。
func isUniqueViolation(err error) bool {
	// mysqlErr 保存mysqlErr，供当前处理流程使用
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}
	// pgErr 保存pgErr，供当前处理流程使用
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	// message 保存消息，供当前处理流程使用
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "constraint failed")
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
// VerifyAndUpgrade 负责VerifyAndUpgrade相关处理。
func (u *Users) VerifyAndUpgrade(ctx context.Context, username, plainPassword string) (*User, bool, error) {
	// user、err 保存user、err，供当前处理流程使用
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
	// matched、needsUpgrade、err 保存matched、needsUpgrade、err，供当前处理流程使用
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
	// hash、err 保存hash、err，供当前处理流程使用
	hash, err := HashPassword(plainPassword)
	if err != nil {
		return false, fmt.Errorf("哈希密码: %w", err)
	}
	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := u.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	// userID 保存用户ID，供当前处理流程使用
	var userID int64
	// res、err 保存res、err，供当前处理流程使用
	res, err := tx.ExecContext(ctx,
		`UPDATE users SET password_hash=?, updated_at=CURRENT_TIMESTAMP WHERE username=?`,
		hash, username)
	if err != nil {
		return false, err
	}
	// n 保存n，供当前处理流程使用
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	if // err 保存err，供当前处理流程使用
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE username=?`, username).Scan(&userID); err != nil {
		return false, err
	}
	if // err 保存err，供当前处理流程使用
	_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// UpdateCredentials 原子更新用户名及可选密码，并撤销该用户的全部登录会话。
func (u *Users) UpdateCredentials(ctx context.Context, userID int64, username, plainPassword string) error {
	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := u.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// exists 保存exists，供当前处理流程使用
	var exists bool
	if // err 保存err，供当前处理流程使用
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE username=? AND id<>?)`, username, userID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrUsernameTaken
	}

	// res 保存响应，供当前处理流程使用
	var res sql.Result
	if plainPassword != "" {
		// hash、hashErr 保存hash、hashErr，供当前处理流程使用
		hash, hashErr := HashPassword(plainPassword)
		if hashErr != nil {
			return fmt.Errorf("哈希密码: %w", hashErr)
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
	if // rows 保存rows，供当前处理流程使用
	rows, _ := res.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	if // err 保存err，供当前处理流程使用
	_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete 删除用户，并撤销该用户的全部登录会话。
func (u *Users) Delete(ctx context.Context, userID int64) error {
	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := u.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if // err 保存err，供当前处理流程使用
	_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return err
	}
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := tx.QueryContext(ctx, `SELECT id FROM cookies WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return err
	}
	// cookieIDs 保存登录凭证IDs，供当前处理流程使用
	var cookieIDs []string
	for rows.Next() {
		// cookieID 保存登录凭证ID，供当前处理流程使用
		var cookieID string
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&cookieID); err != nil {
			_ = rows.Close()
			return err
		}
		cookieIDs = append(cookieIDs, cookieID)
	}
	if // err 保存err，供当前处理流程使用
	err := rows.Close(); err != nil {
		return err
	}
	// 旧 schema 中这些外键没有 ON DELETE CASCADE，必须显式清理。
	for _, query := range []string{
		`DELETE FROM automation_rules WHERE user_id=?`,
		`DELETE FROM cards WHERE user_id=?`,
		`DELETE FROM notification_channels WHERE user_id=?`,
	} {
		if // err 保存err，供当前处理流程使用
		_, err := tx.ExecContext(ctx, query, userID); err != nil {
			return err
		}
	}
	// cookieID 表示当前遍历过程中的登录凭证ID
	for _, cookieID := range cookieIDs {
		if // err 保存err，供当前处理流程使用
		err := deleteCookieTx(ctx, tx, cookieID); err != nil {
			return err
		}
	}
	// res、err 保存res、err，供当前处理流程使用
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id=?`, userID)
	if err != nil {
		return err
	}
	if // rows 保存rows，供当前处理流程使用
	rows, _ := res.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// SetAdmin 标记用户为管理员（init-admin CLI 用）。
func (u *Users) SetAdmin(ctx context.Context, username string) error {
	// err 保存err，供当前处理流程使用
	_, err := u.DB.ExecContext(ctx, `UPDATE users SET is_admin=1 WHERE username=?`, username)
	return err
}

// scanUser 负责scan用户相关处理。
func (u *Users) scanUser(ctx context.Context, query string, args ...any) (*User, error) {
	// usr 保存usr，供当前处理流程使用
	var usr User
	// isActive、isAdmin 保存isActive、isAdmin，供当前处理流程使用
	var isActive, isAdmin int
	// err 保存err，供当前处理流程使用
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
