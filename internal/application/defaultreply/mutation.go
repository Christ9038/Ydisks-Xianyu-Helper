package defaultreply

import (
	"context"
	"errors"
	"fmt"

	"xianyu-go/internal/replyimage"
)

// ErrInvalidReply 表示默认回复的图片来源或本地引用不合法，可安全映射为输入错误。
var ErrInvalidReply = errors.New("默认回复配置无效")

// Draft 是默认回复修改输入；图片指针区分缺省保留与显式清空。
type Draft struct {
	// Enabled 是账号默认回复开关，维持完整表单覆盖语义。
	Enabled bool
	// ReplyContent 是本次保存的回复正文。
	ReplyContent string
	// ReplyImageURL 缺省保留旧 URL，显式空串清除 URL。
	ReplyImageURL *string
	// ReplyImagePath 缺省保留旧本地图，显式空串清除本地图。
	ReplyImagePath *string
	// ReplyOnce 保持账号加会话的一次性回复开关。
	ReplyOnce bool
}

// Update 校验 userID 对 cookieID 的归属，在事务最新快照上应用 draft，防止旧客户端丢失图片配置。
func (s *Service) Update(ctx context.Context, userID int64, cookieID string, draft Draft) error {
	if err := s.ensureOwned(ctx, userID, cookieID); err != nil { // err 是归属或依赖错误。
		return err
	}
	return s.repository.Update(ctx, cookieID, func(current Reply) (Reply, error) {
		// current 是本次事务持有的最新配置，只有明确提供的图片字段才覆盖。
		current.Enabled, current.ReplyContent, current.ReplyOnce = draft.Enabled, draft.ReplyContent, draft.ReplyOnce
		if draft.ReplyImageURL != nil {
			current.ReplyImageURL = *draft.ReplyImageURL
		}
		if draft.ReplyImagePath != nil {
			current.ReplyImagePath = *draft.ReplyImagePath
		}
		if err := replyimage.ValidateSource(current.ReplyImageURL, current.ReplyImagePath); err != nil { // err 是合并后配置的纯输入错误。
			return Reply{}, fmt.Errorf("%w: %s", ErrInvalidReply, err)
		}
		return current, nil
	})
}
