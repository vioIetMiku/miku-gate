import { describe, expect, it } from 'vitest';
import { reputationLabel } from './api';
describe('reputation evidence wording', () => {
  it('never claims a clean IP from missing or partial results', () => {
    expect(reputationLabel('pending')).toContain('未知');
    expect(reputationLabel('failed')).toContain('未知');
    expect(reputationLabel('no_listings_in_checked_sources')).toContain('不代表纯净');
    expect(reputationLabel('listed')).toContain('不等于恶意用途证明');
  });
});
