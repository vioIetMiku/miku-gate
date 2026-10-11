import { DatabaseSync } from 'node:sqlite';
import { createServer } from 'node:net';
import { createServer as createHTTPServer } from 'node:http';
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync, spawn } from 'node:child_process';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const data = join(root, '.cache', 'e2e', `${process.pid}-${Date.now()}`);
mkdirSync(data, { recursive: true });
const binary = join(root, 'bin', process.platform === 'win32' ? 'miku-gate.exe' : 'miku-gate');
const refusingProxy=createServer(socket=>{socket.on('error',()=>socket.destroy());socket.end(Buffer.from([5,255]));});
await new Promise((resolve,reject)=>{refusingProxy.once('error',reject);refusingProxy.listen(0,'127.0.0.1',resolve)});
const refusingProxyPort=refusingProxy.address().port;
const panelTemplate={inbounds:[],outbounds:[{tag:'direct',protocol:'freedom'}],routing:{rules:[]}};
const panelRendered={...panelTemplate,inbounds:[{tag:'fixture-vless',protocol:'vless',listen:'127.0.0.1',port:28443,settings:{clients:[{id:'private-panel-fixture-client'}]},streamSettings:{network:'tcp',security:'tls'}}]};
const panelFixture=createHTTPServer(async(request,response)=>{
 response.setHeader('Content-Type','application/json');const send=obj=>response.end(JSON.stringify({success:true,obj}));
 if(request.url==='/fixture/csrf-token'){response.setHeader('Set-Cookie','session=fixture; Path=/; HttpOnly; SameSite=Strict');send('fixture-csrf-token-atleast16');return}
 if(request.method==='POST'&&request.headers['x-csrf-token']!=='fixture-csrf-token-atleast16'){response.statusCode=403;response.end('{}');return}
 let raw='';for await(const chunk of request){raw+=chunk;if(raw.length>4096){response.statusCode=413;response.end('{}');return}};
 if(request.url==='/fixture/login'){const form=new URLSearchParams(raw);if(form.get('username')!=='fixture-admin'||form.get('password')!=='fixture-private-password'){response.end(JSON.stringify({success:false}));return};send(null);return}
 if(request.url==='/fixture/panel/api/xray/'){send({xraySetting:panelTemplate,outboundTestUrl:'https://fixture.example/'});return}
 if(request.url==='/fixture/panel/api/server/getConfigJson'){send(panelRendered);return}
 response.statusCode=404;response.end('{}');
});
await new Promise((resolve,reject)=>{panelFixture.once('error',reject);panelFixture.listen(0,'127.0.0.1',resolve)});
const panelFixturePort=panelFixture.address().port;
const output = execFileSync(binary, ['init', '--data', data], { encoding: 'utf8', windowsHide: true });
const password = output.match(/密码：([^\r\n]+)/)?.[1];
if (!password) throw new Error('Failed to initialize isolated test account');
writeFileSync(join(root, '.cache', 'e2e-auth.json'), JSON.stringify({ username: 'admin', password, refusingProxyPort,panelFixturePort }), { mode: 0o600 });
// Synthetic cache row for the read-only profile UI, created before the real
// service opens its database. No public node request or mock HTTP API is used.
const seedDB=new DatabaseSync(join(data,'miku-gate.db'));
seedDB.prepare('INSERT INTO nodes VALUES(?,?,?,?,?,?,?,?,?,?,?)').run('profile-check-fixture','fixture-static-profile','8.8.8.8','JP','Fixture country',0,0,0,'https://www.vpngate.net/api/iphone/',new Date().toISOString(),'client\ndev tun\nremote 8.8.8.8 443\n<key>\nUNTRUSTED_TEST_CONFIG_DO_NOT_ECHO\n</key>\n');
seedDB.close();
const server = spawn(binary, ['serve', '--data', data, '--listen', '127.0.0.1:3940'], { stdio: 'inherit', windowsHide: true });
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => server.kill());
process.on('exit', () => { server.kill(); refusingProxy.close();panelFixture.close(); });
server.on('exit', code => {panelFixture.close();refusingProxy.close(()=>process.exit(code ?? 0));});
