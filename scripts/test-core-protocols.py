"""Real pinned sing-box roundtrips, using Go-generated server/share-link configs."""
import http.client
import http.server
import json
from pathlib import Path
import socket
import ssl
import subprocess
import sys
import tempfile
import threading
import time


MARKER=b'miku-protocol-roundtrip'


class Target(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200);self.send_header('Content-Length',str(len(MARKER)));self.end_headers();self.wfile.write(MARKER)
    def log_message(self,*args):pass


def port():
    with socket.socket() as sock:sock.bind(('127.0.0.1',0));return sock.getsockname()[1]


def wait_listener(process,number):
    for _ in range(100):
        if process.poll() is not None:raise RuntimeError('core exited during startup')
        try:
            with socket.create_connection(('127.0.0.1',number),timeout=.2):return
        except OSError:time.sleep(.05)
    raise RuntimeError('core listener did not start')


def main():
    core=sys.argv[1];fixtures=Path(sys.argv[2])
    with tempfile.TemporaryDirectory(prefix='miku-protocol-test-') as tmp:
        root=Path(tmp)
        subprocess.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','1','-subj','/CN=localhost',
                        '-addext','subjectAltName=DNS:localhost','-keyout',str(root/'key.pem'),'-out',str(root/'cert.pem')],
                       stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,check=True)
        target=http.server.ThreadingHTTPServer(('127.0.0.1',0),Target)
        decoy=http.server.ThreadingHTTPServer(('127.0.0.1',0),Target)
        tls=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);tls.minimum_version=ssl.TLSVersion.TLSv1_3
        tls.load_cert_chain(root/'cert.pem',root/'key.pem');tls.set_alpn_protocols(['http/1.1'])
        decoy.socket=tls.wrap_socket(decoy.socket,server_side=True)
        for server in (target,decoy):threading.Thread(target=server.serve_forever,daemon=True).start()
        try:
            for name in ('vless','vmess','trojan'):
                fixture=json.loads((fixtures/(name+'.json')).read_text())
                server=fixture['server'];inbound=server['inbounds'][0]
                inbound['listen']='127.0.0.1';inbound['listen_port']=port()
                inbound['tls']['reality']['handshake']['server_port']=decoy.server_port
                outbound=fixture['client'];outbound['server_port']=inbound['listen_port']
                proxy_port=port()
                client={'log':{'level':'warn'},'inbounds':[{'type':'mixed','listen':'127.0.0.1','listen_port':proxy_port}],
                        'outbounds':[outbound],'route':{'final':outbound['tag']}}
                processes=[];logs=[]
                try:
                    for label,config in (('server',server),('client',client)):
                        path=root/(name+'-'+label+'.json');path.write_text(json.dumps(config))
                        check=subprocess.run([core,'check','-c',str(path)],capture_output=True)
                        if check.returncode:raise RuntimeError(name+' '+label+' config rejected: '+check.stderr.decode(errors='replace')[-1000:])
                        log=(root/(name+'-'+label+'.log')).open('w+b');logs.append(log)
                        process=subprocess.Popen([core,'run','-c',str(path)],stdout=log,stderr=subprocess.STDOUT);processes.append(process)
                        wait_listener(process,inbound['listen_port'] if label=='server' else proxy_port)
                    last=None
                    for _ in range(6):
                        connection=http.client.HTTPConnection('127.0.0.1',proxy_port,timeout=5)
                        try:
                            connection.request('GET',f'http://127.0.0.1:{target.server_port}/marker')
                            response=connection.getresponse()
                            if response.status==200 and response.read()==MARKER:break
                            last=RuntimeError('wrong proxy response')
                        except (OSError,http.client.HTTPException) as error:last=error
                        finally:connection.close()
                        time.sleep(.2)
                    else:raise RuntimeError(name+' REALITY roundtrip failed: '+str(last))
                    print(name+' + '+inbound.get('transport',{}).get('type','tcp')+' + REALITY: actual client/server roundtrip passed',flush=True)
                except Exception:
                    for log in logs:
                        log.flush();log.seek(0);print(log.read()[-2000:].decode(errors='replace'),file=sys.stderr)
                    raise
                finally:
                    for process in processes:
                        process.terminate()
                        try:process.wait(timeout=10)
                        except subprocess.TimeoutExpired:process.kill();process.wait()
                    for log in logs:log.close()
        finally:
            for server in (target,decoy):server.shutdown();server.server_close()


if __name__=='__main__':main()
