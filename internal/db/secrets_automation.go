package db

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateLegacyAutomationDeliveryProofs 加密并校验自动化运行中的历史发货凭证。
func migrateLegacyAutomationDeliveryProofs(ctx context.Context, tx *sql.Tx, codec *secretCodec) error {
	// rows、err 保存待迁移运行凭证的查询游标及错误。
	rows, err := tx.QueryContext(ctx, `SELECT id,delivery_proof FROM automation_runs WHERE delivery_proof IS NOT NULL AND delivery_proof<>''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	// proofs 保存事务内读取的运行凭证，避免游标打开时执行同表更新。
	proofs := make([]struct {
		id    int64
		value string
	}, 0)
	for rows.Next() {
		// proof 保存一条待升级的运行凭证及其主键。
		var proof struct {
			id    int64
			value string
		}
		// err 保存运行凭证扫描错误。
		if err := rows.Scan(&proof.id, &proof.value); err != nil {
			return err
		}
		proofs = append(proofs, proof)
	}
	// err 保存运行凭证查询遍历错误。
	if err := rows.Err(); err != nil {
		return err
	}
	for /* proof 表示当前待升级的运行凭证。 */ _, proof := range proofs {
		// encrypted 保存按运行作用域加密后的凭证。
		encrypted, err := codec.encrypt("automation-delivery-proof", fmt.Sprint(proof.id), proof.value)
		if err != nil {
			return err
		}
		if encrypted == proof.value {
			continue
		}
		// err 保存运行凭证密文写回错误。
		if _, err := tx.ExecContext(ctx, `UPDATE automation_runs SET delivery_proof=? WHERE id=?`, encrypted, proof.id); err != nil {
			return err
		}
	}
	return nil
}
