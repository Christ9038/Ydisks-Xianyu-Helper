-- +goose Up
-- 旧版本逻辑删除遗留了动作及模板变量的卡密外键；升级时补做物理删除。
-- 保留进行中、人工核对、结果未知及仍可重试的运行；未删除规则（包括仅停用规则）不受影响。
-- 动作、模板绑定和已结束运行由现有外键级联清理，卡密与 00042 独立执行守卫保留。
DELETE FROM automation_rules
WHERE deleted_at IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM automation_runs ar
    WHERE ar.rule_id=automation_rules.id
      AND (ar.status IN ('running','needs_review') OR ar.action_started<>0
        OR (ar.status='failed' AND ar.attempt_count<3
          AND ((ar.sent_count=0 AND ar.error_message NOT LIKE '[no_retry]%')
            OR ar.error_message LIKE '[safe_retry]%')))
  );

-- +goose Down
-- 数据清理不可逆；降级不伪造规则或恢复引用，恢复历史记录须使用升级前数据库备份。
SELECT 1;
