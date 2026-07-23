import { expect, test } from 'vitest';
import { selectActivePublishBatch } from './itemPublishBatchState';

test('completed history does not replace the new batch upload flow', () => {
  expect(selectActivePublishBatch([{ id: 'done', status: 'completed' }])).toBeUndefined();
  expect(selectActivePublishBatch([
    { id: 'done', status: 'completed' },
    { id: 'active', status: 'running' },
  ])?.id).toBe('active');
});
