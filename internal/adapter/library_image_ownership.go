package adapter

import (
	"context"
	"errors"

	"xianyu-go/internal/db"
)

// libraryImageOwnedByUser 判断图片库图片是否存在且属于指定用户；imageID 为 0 表示未使用图片库，视为通过。
// 它只读元数据，用于保存配置时拒绝引用他人或已删除的图片。
func libraryImageOwnedByUser(ctx context.Context, store *db.Store, userID, imageID int64) (bool, error) {
	if imageID == 0 {
		return true, nil
	}
	if store == nil || store.LibraryImages == nil {
		return false, errors.New("图片库数据库入口未初始化")
	}
	// image、err 保存按 (用户, 图片) 过滤的元数据查询结果。
	image, err := store.LibraryImages.Get(ctx, userID, imageID)
	if errors.Is(err, db.ErrNotFound) || (err == nil && image == nil) {
		return false, nil
	}
	return err == nil, err
}

// libraryImageOwnedByAccount 判断图片是否属于账号所有者；账号不存在时返回 false，不暴露账号细节。
func libraryImageOwnedByAccount(ctx context.Context, store *db.Store, cookieID string, imageID int64) (bool, error) {
	if imageID == 0 {
		return true, nil
	}
	if store == nil || store.Cookies == nil {
		return false, errors.New("账号数据库入口未初始化")
	}
	// ownerID、ownerErr 保存账号所有者查询结果，不读取凭证。
	ownerID, ownerErr := store.Cookies.GetOwnerID(ctx, cookieID)
	if errors.Is(ownerErr, db.ErrNotFound) {
		return false, nil
	}
	if ownerErr != nil {
		return false, ownerErr
	}
	return libraryImageOwnedByUser(ctx, store, ownerID, imageID)
}
