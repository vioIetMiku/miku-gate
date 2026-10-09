import { describe, expect, it } from 'vitest';
import { filterExits, type Exit } from './api';
const items: Exit[] = [
  {
    id: '1',
    name: '日本开发',
    country: 'JP',
    policy: 'any',
    port: 20001,
    listen: '127.0.0.1',
    username: 'a',
    status: 'stopped',
    reason: '',
    created_at: '',
  },
  {
    id: '2',
    name: '美国测试',
    country: 'US',
    policy: 'any',
    port: 20002,
    listen: '127.0.0.1',
    username: 'b',
    status: 'blocked',
    reason: '',
    created_at: '',
  },
];
describe('出口过滤', () => {
  it('组合名称、国家与端口', () => {
    expect(filterExits(items, '日本', '')).toHaveLength(1);
    expect(filterExits(items, '20002', '')[0].id).toBe('2');
    expect(filterExits(items, '', 'JP')[0].id).toBe('1');
    expect(filterExits(items, '美国', 'JP')).toHaveLength(0);
  });
  it('搜索忽略大小写和两端空白', () => {
    expect(filterExits(items, ' jp ', '')[0].id).toBe('1');
  });
  it('不更改原始配置', () => {
    filterExits(items, '美国', '');
    expect(items).toHaveLength(2);
  });
});

import { afterEach, vi } from 'vitest';
import { api, APIError, setCSRF } from './api';
afterEach(() => {
  vi.unstubAllGlobals();
  setCSRF('');
});
describe('API client', () => {
  it('sends mutations with the current CSRF token', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal('fetch', fetcher);
    setCSRF('test-csrf');
    await api('/exits/one', 'DELETE');
    expect(fetcher.mock.calls[0][1].headers['X-CSRF-Token']).toBe('test-csrf');
    expect(fetcher.mock.calls[0][1].credentials).toBe('same-origin');
  });
  it('rejects non-JSON success responses rather than treating them as data', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('<html>not an API</html>', { status: 200 })),
    );
    await expect(api('/overview')).rejects.toMatchObject({ code: 'invalid_response' });
  });
  it('clears expired session state without exposing credentials', async () => {
    const dispatchEvent = vi.fn();
    vi.stubGlobal('window', { dispatchEvent });
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ code: 'unauthenticated', message: '请登录' }), {
          status: 401,
        }),
      ),
    );
    await expect(api('/exits')).rejects.toBeInstanceOf(APIError);
    expect(dispatchEvent).toHaveBeenCalledOnce();
  });
});
