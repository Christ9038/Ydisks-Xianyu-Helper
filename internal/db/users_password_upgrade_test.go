package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"
)

// passwordUpgradeRowsDriverName 是故障注入驱动在 database/sql 全局注册表中的唯一名称。
const passwordUpgradeRowsDriverName = "password-upgrade-rows-error"

var (
	// passwordUpgradeRowsRegisterOnce 保证测试驱动只向全局 database/sql 注册一次。
	passwordUpgradeRowsRegisterOnce sync.Once
	// errPasswordUpgradeRows 模拟数据库驱动无法报告受影响行数的底层错误。
	errPasswordUpgradeRows = errors.New("rows affected unavailable")
)

// passwordUpgradeRowsDriver 创建只支持旧密码升级写入的故障注入连接。
type passwordUpgradeRowsDriver struct{}

// Open 返回受影响行数读取必然失败的测试连接。
func (passwordUpgradeRowsDriver) Open(_ string) (driver.Conn, error) {
	return passwordUpgradeRowsConn{}, nil
}

// passwordUpgradeRowsConn 实现旧密码升级测试所需的最小数据库连接契约。
type passwordUpgradeRowsConn struct{}

// Prepare 拒绝测试场景不应使用的预编译语句路径。
func (passwordUpgradeRowsConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, driver.ErrSkip
}

// Close 关闭无外部资源的测试连接。
func (passwordUpgradeRowsConn) Close() error {
	return nil
}

// Begin 拒绝测试场景不应启动的显式事务路径。
func (passwordUpgradeRowsConn) Begin() (driver.Tx, error) {
	return nil, driver.ErrSkip
}

// ExecContext 返回写入成功、但无法读取受影响行数的测试结果。
func (passwordUpgradeRowsConn) ExecContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	return passwordUpgradeRowsResult{}, nil
}

// passwordUpgradeRowsResult 模拟驱动执行结果的元数据读取故障。
type passwordUpgradeRowsResult struct{}

// LastInsertId 返回该更新语句未产生自增标识。
func (passwordUpgradeRowsResult) LastInsertId() (int64, error) {
	return 0, nil
}

// RowsAffected 返回预置故障以验证升级错误会传播到认证调用方。
func (passwordUpgradeRowsResult) RowsAffected() (int64, error) {
	return 0, errPasswordUpgradeRows
}

// TestLegacyPasswordUpgradeRejectsConcurrentPasswordChange 验证旧密码升级不会覆盖验证完成后并发写入的新密码。
func TestLegacyPasswordUpgradeRejectsConcurrentPasswordChange(t *testing.T) {
	// store、cleanup 提供迁移后的隔离 SQLite 数据库及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 控制本测试的用户创建、改密和认证数据库操作。
	ctx := context.Background()
	// created、createErr 保存旧账号测试用户的创建结果。
	created, createErr := store.Users.Create(ctx, "legacy-race", "legacy-race@example.com", "bootstrap-password")
	if createErr != nil || !created {
		t.Fatalf("创建旧账号测试用户失败: created=%v err=%v", created, createErr)
	}
	// currentUser、lookupErr 保存准备写入旧摘要的用户记录。
	currentUser, lookupErr := store.Users.GetByUsername(ctx, "legacy-race")
	if lookupErr != nil {
		t.Fatal(lookupErr)
	}
	// oldPassword 是并发场景中的旧口令。
	const oldPassword = "old-legacy-password"
	// legacyHash 是旧口令对应的 SHA-256 摘要。
	legacyHash := legacySHA256(oldPassword)
	if // updateLegacyErr 保存模拟历史数据库摘要时的写入错误。
	_, updateLegacyErr := store.DB.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, legacyHash, currentUser.ID); updateLegacyErr != nil {
		t.Fatal(updateLegacyErr)
	}
	// staleUser、staleLookupErr 模拟登录请求已经读取并验证、但尚未执行升级的旧用户快照。
	staleUser, staleLookupErr := store.Users.GetByUsername(ctx, "legacy-race")
	if staleLookupErr != nil {
		t.Fatal(staleLookupErr)
	}
	// passwordUpdated、passwordUpdateErr 保存并发修改为新密码的结果。
	passwordUpdated, passwordUpdateErr := store.Users.UpdatePassword(ctx, "legacy-race", "new-password")
	if passwordUpdateErr != nil || !passwordUpdated {
		t.Fatalf("并发修改密码失败: updated=%v err=%v", passwordUpdated, passwordUpdateErr)
	}
	// upgradeErr 保存过期登录快照尝试升级旧摘要时的比较并交换冲突。
	upgradeErr := store.Users.upgradeLegacyPassword(ctx, staleUser, oldPassword)
	if !errors.Is(upgradeErr, ErrPasswordMismatch) {
		t.Fatalf("并发改密后旧摘要升级错误=%v，期望密码冲突", upgradeErr)
	}
	// oldUser、oldMatched、oldVerifyErr 验证旧密码没有因升级竞争重新生效。
	oldUser, oldMatched, oldVerifyErr := store.Users.VerifyAndUpgrade(ctx, "legacy-race", oldPassword)
	if oldVerifyErr == nil || oldMatched || oldUser != nil {
		t.Fatalf("旧密码不应恢复: user=%v matched=%v err=%v", oldUser, oldMatched, oldVerifyErr)
	}
	// newUser、newMatched、newVerifyErr 验证并发写入的新密码仍然有效。
	newUser, newMatched, newVerifyErr := store.Users.VerifyAndUpgrade(ctx, "legacy-race", "new-password")
	if newVerifyErr != nil || !newMatched || newUser == nil {
		t.Fatalf("新密码应保持有效: user=%v matched=%v err=%v", newUser, newMatched, newVerifyErr)
	}
}

// TestLegacyPasswordUpgradePropagatesHashAndWriteFailures 验证 bcrypt 生成或 CAS 写入失败时不会伪造登录成功。
func TestLegacyPasswordUpgradePropagatesHashAndWriteFailures(t *testing.T) {
	// store、cleanup 提供迁移后的隔离 SQLite 数据库及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 控制本测试正常准备阶段的数据库操作。
	ctx := context.Background()
	// longPassword 是超过 bcrypt 72 字节上限、但历史 SHA-256 可以保存的兼容口令。
	longPassword := strings.Repeat("长", 25)
	// created、createErr 保存长口令测试用户的创建结果；创建阶段先使用合法短口令。
	created, createErr := store.Users.Create(ctx, "legacy-errors", "legacy-errors@example.com", "bootstrap-password")
	if createErr != nil || !created {
		t.Fatalf("创建升级失败测试用户失败: created=%v err=%v", created, createErr)
	}
	// legacyUser、lookupErr 保存待转换为历史摘要的用户记录。
	legacyUser, lookupErr := store.Users.GetByUsername(ctx, "legacy-errors")
	if lookupErr != nil {
		t.Fatal(lookupErr)
	}
	// legacyHash 保存超长历史口令的 SHA-256 摘要。
	legacyHash := legacySHA256(longPassword)
	if // updateLegacyErr 保存模拟历史摘要的数据库写入错误。
	_, updateLegacyErr := store.DB.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, legacyHash, legacyUser.ID); updateLegacyErr != nil {
		t.Fatal(updateLegacyErr)
	}
	// verifiedUser、matched、verifyErr 保存超长旧密码命中后 bcrypt 升级失败的认证结果。
	verifiedUser, matched, verifyErr := store.Users.VerifyAndUpgrade(ctx, "legacy-errors", longPassword)
	if verifyErr == nil || matched || verifiedUser != nil {
		t.Fatalf("bcrypt 生成失败不得登录成功: user=%v matched=%v err=%v", verifiedUser, matched, verifyErr)
	}
	// storedAfterHashFailure、reloadErr 验证生成失败没有伪造内存或数据库升级结果。
	storedAfterHashFailure, reloadErr := store.Users.GetByUsername(ctx, "legacy-errors")
	if reloadErr != nil || storedAfterHashFailure.PasswordHash != legacyHash {
		t.Fatalf("生成失败后摘要被改变: user=%v err=%v", storedAfterHashFailure, reloadErr)
	}
	// shortPassword 是用于触发数据库写入取消的合法 bcrypt 长度口令。
	const shortPassword = "short-legacy-password"
	// shortLegacyHash 是数据库写入取消场景使用的旧摘要。
	shortLegacyHash := legacySHA256(shortPassword)
	if // updateShortLegacyErr 保存第二个旧摘要的准备错误。
	_, updateShortLegacyErr := store.DB.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, shortLegacyHash, legacyUser.ID); updateShortLegacyErr != nil {
		t.Fatal(updateShortLegacyErr)
	}
	// staleUser、staleLookupErr 保存数据库写入失败测试使用的旧用户快照。
	staleUser, staleLookupErr := store.Users.GetByUsername(ctx, "legacy-errors")
	if staleLookupErr != nil {
		t.Fatal(staleLookupErr)
	}
	// canceledCtx、cancel 创建在 CAS 写入前已取消的生命周期边界。
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	// writeErr 保存取消 Context 导致的旧摘要写入错误。
	writeErr := store.Users.upgradeLegacyPassword(canceledCtx, staleUser, shortPassword)
	if !errors.Is(writeErr, context.Canceled) {
		t.Fatalf("写入取消错误=%v，期望 context.Canceled", writeErr)
	}
	// storedAfterWriteFailure、finalLookupErr 验证数据库写入失败后旧摘要保持不变。
	storedAfterWriteFailure, finalLookupErr := store.Users.GetByUsername(ctx, "legacy-errors")
	if finalLookupErr != nil || storedAfterWriteFailure.PasswordHash != shortLegacyHash {
		t.Fatalf("写入失败后摘要被改变: user=%v err=%v", storedAfterWriteFailure, finalLookupErr)
	}
}

// TestLegacyPasswordUpgradePropagatesRowsAffectedFailure 验证驱动元数据错误不会被当作升级成功。
func TestLegacyPasswordUpgradePropagatesRowsAffectedFailure(t *testing.T) {
	passwordUpgradeRowsRegisterOnce.Do(func() {
		sql.Register(passwordUpgradeRowsDriverName, passwordUpgradeRowsDriver{})
	})
	// sqlDB、openErr 保存故障注入数据库连接池的创建结果。
	sqlDB, openErr := sql.Open(passwordUpgradeRowsDriverName, "")
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer sqlDB.Close()
	// users 提供被测旧密码升级仓储。
	users := &Users{DB: sqlDB}
	// plainPassword 是满足 bcrypt 长度限制的历史口令。
	const plainPassword = "legacy-password"
	// originalHash 是升级前必须保留在内存快照中的 SHA-256 摘要。
	originalHash := legacySHA256(plainPassword)
	// user 模拟已经通过历史摘要校验的用户快照。
	user := &User{ID: 7, PasswordHash: originalHash}
	// upgradeErr 保存读取受影响行数失败时的升级结果。
	upgradeErr := users.upgradeLegacyPassword(context.Background(), user, plainPassword)
	if !errors.Is(upgradeErr, errPasswordUpgradeRows) {
		t.Fatalf("升级错误=%v，期望受影响行数错误", upgradeErr)
	}
	if user.PasswordHash != originalHash {
		t.Fatalf("受影响行数未知时内存摘要被错误更新: %s", user.PasswordHash)
	}
}

// TestMultiDB_LegacyPasswordUpgradeCAS 验证旧密码升级和并发改密保护在全部已配置数据库方言中语义一致。
func TestMultiDB_LegacyPasswordUpgradeCAS(t *testing.T) {
	// target 表示当前执行旧密码升级回归的数据库目标。
	for _, target := range allTestTargets(t) {
		// target 保存当前子测试闭包独占的数据库目标，避免循环变量复用。
		target := target
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 控制当前数据库方言中的用户准备、升级和认证操作。
			ctx := context.Background()
			// store 提供当前方言对应的用户仓储和底层连接。
			store := target.store
			// oldPassword 是两个历史账号共同使用的待升级口令。
			const oldPassword = "multidb-legacy-password"
			// legacyHash 是跨方言准备历史账号时写入的 SHA-256 摘要。
			legacyHash := legacySHA256(oldPassword)

			// upgradedCreated、upgradedCreateErr 保存正常升级账号的创建结果。
			upgradedCreated, upgradedCreateErr := store.Users.Create(ctx, "legacy-upgrade", "legacy-upgrade@example.com", "bootstrap-password")
			if upgradedCreateErr != nil || !upgradedCreated {
				t.Fatalf("创建正常升级账号失败: created=%v err=%v", upgradedCreated, upgradedCreateErr)
			}
			// upgradedBefore、upgradedLookupErr 保存正常升级账号的初始记录。
			upgradedBefore, upgradedLookupErr := store.Users.GetByUsername(ctx, "legacy-upgrade")
			if upgradedLookupErr != nil {
				t.Fatal(upgradedLookupErr)
			}
			if // prepareUpgradeErr 保存当前方言写入历史摘要时的错误。
			_, prepareUpgradeErr := store.DB.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, legacyHash, upgradedBefore.ID); prepareUpgradeErr != nil {
				t.Fatal(prepareUpgradeErr)
			}
			// upgradedUser、upgradedMatched、upgradeErr 保存正常历史账号的认证升级结果。
			upgradedUser, upgradedMatched, upgradeErr := store.Users.VerifyAndUpgrade(ctx, "legacy-upgrade", oldPassword)
			if upgradeErr != nil || !upgradedMatched || upgradedUser == nil || !strings.HasPrefix(upgradedUser.PasswordHash, "$2") {
				t.Fatalf("历史摘要升级失败: user=%v matched=%v err=%v", upgradedUser, upgradedMatched, upgradeErr)
			}

			// raceCreated、raceCreateErr 保存并发改密账号的创建结果。
			raceCreated, raceCreateErr := store.Users.Create(ctx, "legacy-race", "legacy-race@example.com", "bootstrap-password")
			if raceCreateErr != nil || !raceCreated {
				t.Fatalf("创建并发改密账号失败: created=%v err=%v", raceCreated, raceCreateErr)
			}
			// raceBefore、raceLookupErr 保存并发改密账号的初始记录。
			raceBefore, raceLookupErr := store.Users.GetByUsername(ctx, "legacy-race")
			if raceLookupErr != nil {
				t.Fatal(raceLookupErr)
			}
			if // prepareRaceErr 保存并发场景写入历史摘要时的错误。
			_, prepareRaceErr := store.DB.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, legacyHash, raceBefore.ID); prepareRaceErr != nil {
				t.Fatal(prepareRaceErr)
			}
			// staleUser、staleLookupErr 保存并发改密前读取的过期登录快照。
			staleUser, staleLookupErr := store.Users.GetByUsername(ctx, "legacy-race")
			if staleLookupErr != nil {
				t.Fatal(staleLookupErr)
			}
			// passwordUpdated、passwordUpdateErr 保存当前方言写入新密码的结果。
			passwordUpdated, passwordUpdateErr := store.Users.UpdatePassword(ctx, "legacy-race", "new-password")
			if passwordUpdateErr != nil || !passwordUpdated {
				t.Fatalf("并发修改密码失败: updated=%v err=%v", passwordUpdated, passwordUpdateErr)
			}
			// conflictErr 保存过期快照执行比较并交换时的预期冲突。
			conflictErr := store.Users.upgradeLegacyPassword(ctx, staleUser, oldPassword)
			if !errors.Is(conflictErr, ErrPasswordMismatch) {
				t.Fatalf("并发改密后旧摘要升级错误=%v，期望密码冲突", conflictErr)
			}
			// currentUser、currentMatched、currentVerifyErr 验证新密码没有被过期升级覆盖。
			currentUser, currentMatched, currentVerifyErr := store.Users.VerifyAndUpgrade(ctx, "legacy-race", "new-password")
			if currentVerifyErr != nil || !currentMatched || currentUser == nil {
				t.Fatalf("新密码应保持有效: user=%v matched=%v err=%v", currentUser, currentMatched, currentVerifyErr)
			}
		})
	}
}
