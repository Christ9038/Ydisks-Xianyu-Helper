export interface PublishBatchSummary {
  id?: string;
  status?: string;
}

export const selectActivePublishBatch = <T extends PublishBatchSummary>(batches: T[]): T | undefined =>
  batches.find(batch => batch.status === 'running' || batch.status === 'canceling');
