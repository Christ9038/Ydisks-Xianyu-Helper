import { describe, expect, test } from 'vitest';
import { collectionFrom, objectFrom } from './contract';

/** ContractRow 是集合归一测试使用的最小具名行模型。 */
interface ContractRow {
  /** id 是测试集合项的稳定标识。 */
  id: number;
}

describe('HTTP transport contract normalizers', () => {
	test('集合归一器接受数组、null、命名包裹和双层 data 包裹', /* 回调验证集合响应的兼容归一规则。 */ () => {
		// rows 是不同发布版本可能返回的集合响应形状。
		const rows = [{
			// id 是测试集合项的稳定标识。
			id: 1,
		}];
		expect(collectionFrom<ContractRow>(rows)).toEqual(rows);
		expect(collectionFrom<ContractRow>(null)).toEqual([]);
		expect(collectionFrom<ContractRow>({ items: rows }, ['items'])).toEqual(rows);
		expect(collectionFrom<ContractRow>({ data: { results: rows } }, ['results'])).toEqual(rows);
  });

	test('对象归一器保留直接对象并展开 data/result 历史包裹', /* 回调验证对象响应的兼容归一规则。 */ () => {
		// direct 是当前版本直接返回的对象契约。
		const direct = { /** enabled 表示测试对象的启用状态。 */ enabled: true };
    expect(objectFrom<typeof direct>(direct)).toEqual(direct);
    expect(objectFrom<typeof direct>({ data: direct })).toEqual(direct);
    expect(objectFrom<typeof direct>({ result: direct })).toEqual(direct);
    expect(objectFrom<typeof direct>(null)).toBeUndefined();
  });
});
