import { describe, expect, it } from 'vitest';
import { shouldUpdateAccountPause } from './accountPause';

describe('shouldUpdateAccountPause', () => {
  it('does not silently reapply the same duration after a pause has expired', () => {
    expect(shouldUpdateAccountPause(60, { pause_duration: 60, paused: false })).toBe(false);
  });

  it('does not restart an active unchanged pause', () => {
    expect(shouldUpdateAccountPause(60, { pause_duration: 60, paused: true })).toBe(false);
  });

  it('sends changed values including explicit resume', () => {
    expect(shouldUpdateAccountPause(30, { pause_duration: 60, paused: true })).toBe(true);
    expect(shouldUpdateAccountPause(0, { pause_duration: 60, paused: true })).toBe(true);
  });
});
