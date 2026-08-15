package db

import (
	"context"
	"database/sql"
	"errors"
)

// OrderRefreshJobs 持久化订单刷新后台任务及其租约状态。
type OrderRefreshJobs struct {
	// DB 保存任务表使用的数据库连接。
	DB *sql.DB
}

// OrderRefreshJob 是订单刷新任务的持久化模型。
type OrderRefreshJob struct {
	// ID 是任务唯一标识。
	ID string
	// UserID 是任务所属用户标识。
	UserID int64
	// CookieID 是可选的目标账号标识。
	CookieID string
	// FilterStatus 是订单状态筛选条件。
	FilterStatus string
	// Status 是 queued/running/succeeded/failed/cancelled 之一。
	Status string
	// ResultJSON 保存成功后的具名刷新结果 JSON。
	ResultJSON string
	// ErrorMessage 保存任务失败原因。
	ErrorMessage string
	// WorkerToken 是当前执行者租约令牌。
	WorkerToken string
	// LeaseExpiresAt 是租约过期 Unix 秒时间戳。
	LeaseExpiresAt int64
	// CreatedAt 是任务创建时间。
	CreatedAt string
	// UpdatedAt 是任务最后更新时间。
	UpdatedAt string
}

// Create 创建一个 queued 状态的订单刷新任务。
func (j *OrderRefreshJobs) Create(ctx context.Context, job *OrderRefreshJob) error {
	if job.Status == "" {
		job.Status = "queued"
	}
	if job.ResultJSON == "" {
		job.ResultJSON = "{}"
	}
	// err 表示创建订单刷新任务的数据库错误。
	_, err := j.DB.ExecContext(ctx,
		`INSERT INTO order_refresh_jobs
		 (id,user_id,cookie_id,filter_status,status,result_json,error_message,worker_token,lease_expires_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		job.ID, job.UserID, job.CookieID, job.FilterStatus, job.Status, job.ResultJSON,
		job.ErrorMessage, job.WorkerToken, job.LeaseExpiresAt)
	return err
}

// Get 按用户读取订单刷新任务，防止跨用户查询任务结果。
func (j *OrderRefreshJobs) Get(ctx context.Context, userID int64, id string) (*OrderRefreshJob, error) {
	// job 保存读取到的任务模型。
	job := &OrderRefreshJob{}
	// err 保存任务查询错误。
	err := j.DB.QueryRowContext(ctx,
		`SELECT id,user_id,cookie_id,filter_status,status,COALESCE(result_json,'{}'),
		        COALESCE(error_message,''),COALESCE(worker_token,''),COALESCE(lease_expires_at,0),created_at,updated_at
		   FROM order_refresh_jobs WHERE id=? AND user_id=?`, id, userID).Scan(
		&job.ID, &job.UserID, &job.CookieID, &job.FilterStatus, &job.Status, &job.ResultJSON,
		&job.ErrorMessage, &job.WorkerToken, &job.LeaseExpiresAt, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return job, nil
}

// Claim 将 queued 任务原子切换为 running 并写入租约。
func (j *OrderRefreshJobs) Claim(ctx context.Context, id, token string, leaseExpiresAt int64) (bool, error) {
	// result、err 保存抢占更新结果及错误。
	result, err := j.DB.ExecContext(ctx,
		`UPDATE order_refresh_jobs
		    SET status='running',worker_token=?,lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		  WHERE id=? AND status='queued'`, token, leaseExpiresAt, id)
	if err != nil {
		return false, err
	}
	// affected、err 保存抢占更新影响行数及错误。
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// Complete 在租约令牌匹配时写入任务终态，过期 worker 不能覆盖新执行者结果。
func (j *OrderRefreshJobs) Complete(ctx context.Context, id, token, status, resultJSON, errorMessage string) (bool, error) {
	// result、err 保存任务终态更新结果及错误。
	result, err := j.DB.ExecContext(ctx,
		`UPDATE order_refresh_jobs
		    SET status=?,result_json=?,error_message=?,worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		  WHERE id=? AND status='running' AND worker_token=?`, status, resultJSON, errorMessage, id, token)
	if err != nil {
		return false, err
	}
	// affected、err 保存终态更新影响行数及错误。
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// Recoverable 返回租约已过期的 running 任务，供恢复扫描器重新排队。
func (j *OrderRefreshJobs) Recoverable(ctx context.Context, now int64, limit int) ([]OrderRefreshJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	// rows、err 保存可恢复任务查询结果及错误。
	rows, err := j.DB.QueryContext(ctx,
		`SELECT id,user_id,cookie_id,filter_status,status,COALESCE(result_json,'{}'),
		        COALESCE(error_message,''),COALESCE(worker_token,''),COALESCE(lease_expires_at,0),created_at,updated_at
		   FROM order_refresh_jobs
		  WHERE status='running' AND lease_expires_at>0 AND lease_expires_at<=?
		  ORDER BY updated_at ASC,id ASC LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// jobs 保存可恢复任务列表。
	jobs := make([]OrderRefreshJob, 0, limit)
	for rows.Next() {
		// job 保存当前可恢复任务。
		var job OrderRefreshJob
		// err 表示扫描可恢复任务的数据库错误。
		if err := rows.Scan(&job.ID, &job.UserID, &job.CookieID, &job.FilterStatus, &job.Status,
			&job.ResultJSON, &job.ErrorMessage, &job.WorkerToken, &job.LeaseExpiresAt, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// RequeueExpired 将过期运行任务恢复为 queued，并清理旧 worker 租约。
func (j *OrderRefreshJobs) RequeueExpired(ctx context.Context, id string, now int64) (bool, error) {
	// result、err 保存任务重新入队结果及错误。
	result, err := j.DB.ExecContext(ctx,
		`UPDATE order_refresh_jobs
		    SET status='queued',worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		  WHERE id=? AND status='running' AND lease_expires_at>0 AND lease_expires_at<=?`, id, now)
	if err != nil {
		return false, err
	}
	// affected、err 保存重新入队影响行数及错误。
	affected, err := result.RowsAffected()
	return affected == 1, err
}
