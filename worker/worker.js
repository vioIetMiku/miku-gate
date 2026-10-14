// fanout 拉 VPN Gate 节点列表的反代兜底。只做这一件事。
//
//   GET /vpngate  -> https://www.vpngate.net/api/iphone/
//
// 要带 X-Fanout-Key（或 ?k=）匹配 ACCESS_KEY，不带就一律 404。
// 这层只挡公网无差别扫描和被当免费代理白嫖，挡不住读源码的人。

const UPSTREAM = 'https://www.vpngate.net/api/iphone/';

// 定长比较，别让响应时间泄漏前缀
function keyMatches(got, want) {
  if (!got || !want || got.length !== want.length) return false;
  let diff = 0;
  for (let i = 0; i < got.length; i++) diff |= got.charCodeAt(i) ^ want.charCodeAt(i);
  return diff === 0;
}

// 扫到的人和密钥不对的人看到的是同一个页面，别暴露这里有东西
function notFound() {
  return new Response('404 Not Found\n', {
    status: 404,
    headers: { 'content-type': 'text/plain' },
  });
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);

    if (request.method !== 'GET' && request.method !== 'HEAD') {
      return notFound();
    }
    if (url.pathname !== '/vpngate' && url.pathname !== '/vpngate/') {
      return notFound();
    }

    const key = request.headers.get('x-fanout-key') || url.searchParams.get('k') || '';
    if (!keyMatches(key, env.ACCESS_KEY || '')) {
      return notFound();
    }

    const headers = new Headers();
    // 只透传必要请求头，别把访问者的 cookie/authorization 送去上游
    for (const k of ['accept', 'accept-encoding', 'user-agent']) {
      const v = request.headers.get(k);
      if (v) headers.set(k, v);
    }
    if (!headers.has('user-agent')) headers.set('user-agent', 'fanout-proxy');

    let resp;
    try {
      resp = await fetch(UPSTREAM, {
        method: request.method,
        headers,
        redirect: 'follow',
        cf: { cacheTtl: 0 },
      });
    } catch (err) {
      return new Response('upstream error: ' + err + '\n', { status: 502 });
    }

    const out = new Headers(resp.headers);
    out.delete('set-cookie');
    // 上游带 max-age=14400，透传出去 CF 边缘会缓存这个响应，
    // 后续没带密钥的请求直接命中缓存、绕过上面的鉴权。必须剥掉。
    for (const k of ['cache-control', 'expires', 'last-modified', 'etag', 'age']) {
      out.delete(k);
    }
    out.set('cache-control', 'no-store');
    out.set('access-control-allow-origin', '*');
    return new Response(resp.body, { status: resp.status, headers: out });
  },
};
