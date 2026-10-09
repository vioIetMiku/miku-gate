import assert from 'node:assert/strict';
import { request as httpRequest } from 'node:http';
import { execFileSync, spawn } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const data = join(root, '.cache', 'smoke', `${process.pid}-${Date.now()}`);
mkdirSync(data, { recursive: true });
const binary = join(root, 'bin', process.platform === 'win32' ? 'miku-gate.exe' : 'miku-gate');
const output = execFileSync(binary, ['init', '--data', data], { encoding: 'utf8', windowsHide: true });
const password = output.match(/密码：([^\r\n]+)/)?.[1];
assert.ok(password, 'init must print a one-time random password');
const child = spawn(binary, ['serve', '--data', data, '--listen', '127.0.0.1:0'], { windowsHide: true, stdio: ['ignore', 'ignore', 'pipe'] });
let stderr = '';
child.stderr.on('data', chunk => { stderr += chunk; });
let cookie = ''; let csrf = ''; let checks = 0;
try {
  const base = await new Promise((resolveBase, reject) => {
    const timer = setTimeout(() => reject(new Error('server did not become ready')), 10000);
    child.once('error', e => { clearTimeout(timer); reject(e); });
    child.once('exit', code => { clearTimeout(timer); reject(new Error(`server exited ${code}: ${stderr}`)); });
    child.stderr.on('data', () => { const match = stderr.match(/http:\/\/(127\.0\.0\.1:\d+)/); if (match) { clearTimeout(timer); resolveBase(`http://${match[1]}`); } });
  });
  async function request(path, method = 'GET', body, headers = {}) {
    return fetch(`${base}${path}`, { method, headers: { 'content-type': 'application/json', ...(cookie ? { Cookie: cookie } : {}), ...(csrf && method !== 'GET' ? { 'X-CSRF-Token': csrf } : {}), ...headers }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(10000) });
  }
  function check(actual, expected, message) { assert.equal(actual, expected, message); checks++; }
  check((await request('/api/exits')).status, 401, 'anonymous API requests must fail');
  let r = await request('/'); check(r.status, 200, 'embedded frontend must be served');
  const html = await r.text(); const asset = html.match(/src="(\/assets\/[^\"]+)"/); assert.ok(asset, 'built JS asset should exist');
  check((await request(asset[1])).status, 200, 'embedded JS asset must be served');
  const hostStatus = await new Promise((resolveStatus, reject) => { const req = httpRequest(`${base}/api/health`, { headers: { Host: 'evil.example' } }, response => { response.resume(); resolveStatus(response.statusCode); }); req.on('error', reject); req.end(); });
  check(hostStatus, 403, 'untrusted Host must fail');
  r = await request('/api/auth/login', 'POST', { username: 'admin', password }); check(r.status, 200, 'login with initialized credentials');
  cookie = r.headers.getSetCookie()[0].split(';')[0]; csrf = (await r.json()).csrf; assert.ok(csrf);
  check((await request('/api/exits', 'POST', { name: '日本测试', country: 'JP' }, { 'X-CSRF-Token': '' })).status, 403, 'CSRF is required');
  check((await request('/api/exits', 'POST', { name: '日本测试', country: 'JP' }, { Origin: 'https://evil.example' })).status, 403, 'cross-site request denied');
  r = await request('/api/exits', 'POST', { name: '日本测试', country: 'JP', policy: 'any' }); check(r.status, 201, 'create exit configuration');
  const created = await r.json(); assert.ok(created.password); const id = created.exit.id; const port = created.exit.port;
  r = await request('/api/exits'); const exits = await r.text(); check(exits.includes(created.password), false, 'one-time secret must not reappear');
  check((await request(`/api/exits/${id}/start`, 'POST')).status, 409, 'unimplemented executor must not fake success');
  check((await request('/api/overview').then(r => r.json())).active_count, 0, 'no real tunnel may be reported');
  check((await request(`/api/exits/${id}`, 'PATCH', { name: '韩国测试', country: 'KR', policy: 'residential_strict' })).status, 204, 'edit configuration');
  check((await request('/api/exits').then(r => r.json()))[0].port, port, 'editing must preserve entrance port');
  check((await request(`/api/exits/${id}/credentials`, 'POST')).status, 200, 'credential rotation');
  check((await request(`/api/exits/${id}/stop`, 'POST')).status, 204, 'stop configuration');
  check((await request('/api/diagnostics').then(r => r.json())).executor_available, false, 'diagnostics are honest');
  check((await request(`/api/exits/${id}`, 'DELETE')).status, 204, 'delete isolated test configuration');
  check((await request('/api/exits').then(r => r.json())).length, 0, 'configuration deleted');
  check((await request('/api/events').then(r => r.json())).length >= 6, true, 'audit trail persisted');
  check((await request('/api/auth/logout', 'POST')).status, 204, 'logout');
  check((await request('/api/exits')).status, 401, 'revoked session rejected');
  console.log(`PASS: ${checks} HTTP/CLI smoke checks (isolated database, no VPN or proxy listener).`);
} finally {
  child.kill();
}
