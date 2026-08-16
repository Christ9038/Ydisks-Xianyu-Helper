package items

import (
	"context"
	"errors"
)

// BatchPreviewPersistenceBatch 是预检批次的非敏感持久化元数据。
type BatchPreviewPersistenceBatch struct {
	// ID 是后续启动发布任务使用的批次标识。
	ID string
	// UserID 是发起批次导入的用户标识。
	UserID int64
	// DefaultCookieID 是未指定账号行使用的默认账号。
	DefaultCookieID string
	// Filename 是用户上传的原始表格文件名。
	Filename string
	// UploadDir 是受控上传目录，供后续 worker 读取图片。
	UploadDir string
	// Location 是批次统一使用的发货地。
	Location Location
	// Status 是创建时的批次状态。
	Status string
}

// BatchPreviewPersistenceRepository 定义预检批次落库所需的最小数据库端口。
type BatchPreviewPersistenceRepository interface {
	// CreateBatch 在一个持久化边界内创建批次和全部明细。
	CreateBatch(context.Context, BatchPreviewPersistenceBatch, []BatchPreviewRow) error
	// RecountBatch 重算批次的成功和失败统计。
	RecountBatch(context.Context, string) error
}

// BatchPreviewPersistenceResult 是预检接口返回的应用层结果。
type BatchPreviewPersistenceResult struct {
	// Success 表示预检结果已成功持久化。
	Success bool
	// PreviewID 是后续启动批量发布使用的批次标识。
	PreviewID string
	// Total 是预检行总数。
	Total int
	// Valid 是没有校验错误的行数。
	Valid int
	// Invalid 是包含校验错误的行数。
	Invalid int
	// Rows 是逐行预检结果。
	Rows []BatchPreviewRow
}

// BatchPreviewPersistenceService 负责预检结果计数和批次落库编排。
type BatchPreviewPersistenceService struct {
	// repository 提供数据库批次写入能力。
	repository BatchPreviewPersistenceRepository
}

// NewBatchPreviewPersistenceService 创建预检持久化应用服务。
func NewBatchPreviewPersistenceService(repository BatchPreviewPersistenceRepository) (*BatchPreviewPersistenceService, error) {
	if repository == nil {
		return nil, errors.New("预检持久化端口不能为空")
	}
	return &BatchPreviewPersistenceService{repository: repository}, nil
}

// Persist 持久化逐行预检结果并返回兼容前端的应用模型。
func (service *BatchPreviewPersistenceService) Persist(ctx context.Context, batch BatchPreviewPersistenceBatch, rows []BatchPreviewRow) (BatchPreviewPersistenceResult, error) {
	if service == nil || service.repository == nil {
		return BatchPreviewPersistenceResult{}, errors.New("预检持久化服务未初始化")
	}
	if len(rows) == 0 {
		return BatchPreviewPersistenceResult{}, ErrBatchPreviewNoRows
	}
	// valid 和 invalid 保存预检通过与失败的行数。
	valid := 0
	// invalid 保存包含校验错误的预检行数。
	invalid := 0
	// row 表示当前参与统计的预检行。
	for _, row := range rows {
		if len(row.Errors) == 0 {
			valid++
		} else {
			invalid++
		}
	}
	if batch.Status == "" {
		batch.Status = "preview"
	}
	// err 表示批次及明细写入错误。
	if err := service.repository.CreateBatch(ctx, batch, rows); err != nil {
		return BatchPreviewPersistenceResult{}, errors.Join(errors.New("保存预检结果失败"), err)
	}
	// recountErr 保存统计重算结果；旧接口在批次已写入后仍返回预检成功，因此保留该兼容语义。
	_ = service.repository.RecountBatch(ctx, batch.ID)
	return BatchPreviewPersistenceResult{Success: true, PreviewID: batch.ID, Total: len(rows), Valid: valid, Invalid: invalid, Rows: rows}, nil
}
