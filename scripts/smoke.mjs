import assert from 'node:assert/strict';
import { request as httpRequest } from 'node:http';
import { execFileSync, spawn, spawnSync } from 'node:child_process';
import { mkdirSync, existsSync } from 'node:fs';
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
  const competing=spawnSync(binary,['serve','--data',data,'--listen','127.0.0.1:0'],{encoding:'utf8',windowsHide:true,timeout:5000});
  check(competing.status,1,'one control service per data directory');
  check(competing.stderr.includes('已有控制服务'),true,'competing service fails on the lease, not a bind collision');
  check((await request('/api/exits')).status, 401, 'anonymous API requests must fail');
  check((await request('/api/exits/missing_exit/network-plan')).status, 401, 'anonymous network plans must fail');
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
  r = await request(`/api/exits/${id}/network-plan`); check(r.status, 200, 'authenticated network-plan preview');
  const networkPlan = await r.json();
  check(networkPlan.stage, 'quarantine-only', 'plan must not imply a running tunnel');
  check(networkPlan.proxy_enabled || networkPlan.tunnel_enabled || networkPlan.host_routes_changed || networkPlan.host_firewall_changed, false, 'plan has no data path or host routing changes');
  const planArgs = [`--instance=${networkPlan.instance_id}`, `--exit=${id}`];
  const cliPlan = JSON.parse(execFileSync(binary, ['sandbox', 'plan', ...planArgs], { encoding: 'utf8', windowsHide: true }));
  const instanceID = execFileSync(binary, ['instance', '--data', data], { encoding: 'utf8', windowsHide: true }).trim();
  check(instanceID, networkPlan.instance_id, 'instance CLI matches the authenticated plan');
  const missingData = join(data, 'missing-instance');
  const missingInstance = spawnSync(binary, ['instance', '--data', missingData], { encoding: 'utf8', windowsHide: true });
  check(missingInstance.status, 1, 'instance lookup does not silently initialize missing data');
  check(existsSync(missingData), false, 'failed instance lookup creates no directory');
  check(cliPlan.namespace, networkPlan.namespace, 'CLI and web use the same namespace identity');
  check(cliPlan.nft_ruleset, networkPlan.nft_ruleset, 'CLI and web use the same guard policy');
  const denied = spawnSync(binary, ['sandbox', 'prepare', ...planArgs], { encoding: 'utf8', windowsHide: true });
  check(denied.status, 1, 'privileged mutation requires explicit --apply');
  check((await request('/api/exits').then(r => r.json()))[0].status, 'stopped', 'preview leaves exit state unchanged');
  check((await request(`/api/exits/${id}/start`, 'POST')).status, 409, 'data plane must remain gated');
  check((await request('/api/overview').then(r => r.json())).active_count, 0, 'no real tunnel may be reported');
  check((await request(`/api/exits/${id}`, 'PATCH', { name: '韩国测试', country: 'KR', policy: 'residential_strict' })).status, 204, 'edit configuration');
  check((await request('/api/exits').then(r => r.json()))[0].port, port, 'editing must preserve entrance port');
  check((await request(`/api/exits/${id}/credentials`, 'POST')).status, 200, 'credential rotation');
  check((await request(`/api/exits/${id}/stop`, 'POST')).status, 204, 'stop configuration');
  check((await request(`/api/exits/${id}/runtime`).then(r=>r.json())).proxy_listening,false,'stopped runtime has no listener');
  check((await request(`/api/exits/${id}/upstream`).then(r=>r.json())).configured,false,'empty upstream is explicit');
  const upstream={kind:'http',host:'127.0.0.1',port:10999,username:'smoke-up',password:'ephemeral-upstream-password'};
  check((await request(`/api/exits/${id}/upstream`,'PUT',upstream)).status,400,'loopback requires explicit confirmation');
  check((await request(`/api/exits/${id}/upstream`,'PUT',{...upstream,allow_loopback:true})).status,204,'save encrypted upstream without opening a connection');
  const savedUpstream=await request(`/api/exits/${id}/upstream`).then(r=>r.json());
  check(savedUpstream.has_password,true,'saved upstream reports credential presence');
  check(JSON.stringify(savedUpstream).includes(upstream.password),false,'upstream secret never returns');
  check((await request('/api/exits').then(r=>r.json())).find(e=>e.id===id).upstream_kind,'http','driver type visible without secrets');
  check((await request(`/api/exits/${id}/upstream`,'PUT',{...upstream,password:'',reuse_password:true,port:11000,allow_loopback:true})).status,409,'credential reuse cannot change recipient');
  check((await request(`/api/exits/${id}/upstream`,'DELETE')).status,204,'remove upstream while stopped');

  check((await request('/api/executor').then(r => r.json())).configured, false, 'sandbox IPC is opt-in');
  check((await request(`/api/exits/${id}/sandbox`)).status, 503, 'no implicit privileged sandbox inspection');
  check((await request(`/api/exits/${id}/sandbox/prepare`, 'POST', { confirm: true })).status, 503, 'no implicit privileged preparation');
  check((await request(`/api/exits/${id}/sandbox/prepare`, 'POST', {})).status, 400, 'sandbox action requires confirmation');
  const inventory = await request('/api/host/inventory').then(r => r.json());
  check(inventory.read_only, true, 'host inventory is read-only');
  check(inventory.components.every(c => c.reuse_ready === false), true, 'inventory cannot authorize reuse');
  check((await request('/api/diagnostics').then(r => r.json())).executor_available, false, 'diagnostics are honest');
  check((await request(`/api/exits/${id}`, 'DELETE')).status, 204, 'delete isolated test configuration');
  check((await request('/api/exits').then(r => r.json())).length, 0, 'configuration deleted');
  check((await request(`/api/exits/${id}/network-plan`)).status, 404, 'deleted exit cannot retain a plan');
  check((await request('/api/events').then(r => r.json())).length >= 6, true, 'audit trail persisted');
  check((await request('/api/auth/logout', 'POST')).status, 204, 'logout');
  check((await request('/api/exits')).status, 401, 'revoked session rejected');
  console.log(`PASS: ${checks} HTTP/CLI smoke checks (isolated database, no public network or active proxy).`);
} finally {
  child.kill();
}
