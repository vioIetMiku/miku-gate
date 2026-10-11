#!/usr/bin/env python3
"""Opt-in real encrypted ingress -> actual isolated official VPN Gate acceptance.

Requires authorized Linux root coordinator and a bounded cgroup. Control/core
UID65534, OpenVPN65533, worker65532; only loopback ingress/client listeners. At
most two public VPN candidates and one explicit REALITY target; no service apply.
"""
import argparse
import contextlib
import hashlib
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import stat
import subprocess
import sys
import time

spec=importlib.util.spec_from_file_location("public_vpn",Path(__file__).with_name("vps-vpngate-live.py"))
public=importlib.util.module_from_spec(spec);spec.loader.exec_module(public)
live=public.live

def checked_binary(path,digest):
    p=Path(path);info=p.lstat()
    live.require(p.resolve(strict=True)==p and stat.S_ISREG(info.st_mode) and info.st_uid==0 and info.st_nlink==1 and stat.S_IMODE(info.st_mode)==0o555,"unexpected pinned executable")
    live.require(re.fullmatch(r"[a-f0-9]{64}",digest) and hashlib.sha256(p.read_bytes()).hexdigest()==digest,"binary hash mismatch")
    return p

def free_port():
    with live.socket.socket() as sock:
        sock.bind(("127.0.0.1",0));return sock.getsockname()[1]

class EncryptedControl(public.UserControl):
    cores={}
    def argv(self,command):
        args=super().argv(command)
        if command=="serve":
            args += ["--xray-core",str(self.cores["xray"]),"--sing-box-core",str(self.cores["sing-box"])]
        return args

class CoreClient:
    def __init__(self,path,core,raw,port):
        args=["run","-config","stdin:"] if core=="xray" else ["run","-c","/dev/stdin"]
        self.process=subprocess.Popen(["/usr/bin/setpriv","--reuid=65534","--regid=65534","--clear-groups","--no-new-privs","--",str(path),*args],cwd="/",env=live.ENV,stdin=subprocess.PIPE,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
        try:
            self.process.stdin.write(raw);self.process.stdin.close()
            deadline=time.monotonic()+8
            while time.monotonic()<deadline:
                live.require(self.process.poll() is None,"generated client exited before listening")
                if not live.connection_refused(("127.0.0.1",port)):return
                time.sleep(0.05)
            raise RuntimeError("generated core client readiness timeout")
        except BaseException:
            with contextlib.suppress(Exception):self.close()
            raise
    def close(self):
        if self.process.poll() is None:self.process.terminate()
        try:self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.process.kill();self.process.wait(timeout=3)
            raise RuntimeError("owned core client failed orderly stop") from None

def step(core,phase):
    # Closed set lifecycle observations, no credentials/config/IPs.
    print("ENCRYPTED_VPN_STEP " + json.dumps({"core":core,"phase":phase}),flush=True)

def encrypted_path(control,created,state):
    exit_id=created["exit"]["id"];prefix="/api/exits/"+exit_id
    observations=[]
    expected_ip=state["proof"]["exit_ip"]
    for core in ("xray","sing-box"):
        # Saving must reserve a stopped exit: no config/auth recipient mutation
        # while the previous real VPN listener is running.
        step(core,"stopping-exit-for-save")
        control.request("POST",prefix+"/stop",expect=204)
        port=free_port();client_port=free_port()
        request={"core":core,"template":"vless-reality-vision","address":"127.0.0.1","listen":"127.0.0.1","port":port,"server_name":"www.microsoft.com","reality_hostname":"www.microsoft.com"}
        step(core,"saving-verified-material")
        saved=control.request("PUT",prefix+"/ingress",{"request":request,"exit_password":created["password"],"confirm":True,"confirm_network":True},expect=200)
        live.require(saved["configured"] and not saved["applied"],"save pretended applied")
        revealed=control.request("POST",prefix+"/ingress/client",{"confirm":True},expect=200)
        live.require(not revealed["server_private_key_returned"] and not revealed["proxy_password_returned"],"private material leaked to client")
        config=json.loads(revealed["client_config"])
        live.require("privateKey" not in revealed["client_config"] and "private_key" not in revealed["client_config"] and created["password"] not in revealed["client_config"],"server/exit secret in client config")
        # Prove durable identity through actual control process restart. No
        # privileged executor replacement; root daemon remains the same instance.
        before_uri=revealed["uri"]
        step(core,"restarting-control-checking-persistence")
        control.stop();control.start()
        persisted=control.request("POST",prefix+"/ingress/client",{"confirm":True},expect=200)
        live.require(persisted["uri"]==before_uri,"control restart replaced client credentials")
        step(core,"starting-isolated-vpn")
        running=control.request("POST",prefix+"/start",expect=200)
        live.require(running["proof"]["kernel_verified"] and running["proof"]["dns_verified"],"restored VPN proof missing")
        step(core,"starting-owned-core")
        core_state=control.request("POST",prefix+"/ingress/start",{"confirm":True},expect=200)
        live.require(core_state["phase"]=="running" and core_state["listener_owned"] and core_state["core_validated"] and not core_state["data_verified"],"native runtime overstated evidence")
        user="private-client";password=secrets.token_urlsafe(18)
        inbound=config["inbounds"][0]
        if core=="xray":
            inbound["port"]=client_port;inbound["settings"]={"auth":"password","accounts":[{"user":user,"pass":password}],"udp":False}
        else:
            inbound["listen_port"]=client_port;inbound["users"]=[{"username":user,"password":password}]
        step(core,"starting-generated-client")
        client=CoreClient(control.cores[core],core,json.dumps(config).encode(),client_port)
        try:
            step(core,"business-https-through-encrypted-path")
            body=live.fetch_via(("127.0.0.1",client_port),"example.com","/",user,password,socks=True)
            live.require(b"Example Domain" in body,"encrypted ingress business HTTPS failed")
            step(core,"checking-business-public-exit-ip")
            raw_ip=live.fetch_via(("127.0.0.1",client_port),"api.ipify.org","/",user,password,socks=True).decode().strip()
            ip=ipaddress.ip_address(raw_ip)
            live.require(ip.version==4 and ip.is_global and raw_ip==running["proof"]["exit_ip"],"encrypted business path exit IP mismatched VPN proof")
            step(core,"checking-live-mutation-lock")
            status,_=control.call("PUT",prefix+"/ingress",{"request":request,"exit_password":created["password"],"confirm":True,"confirm_network":True})
            live.require(status==409,"live ingress changed without stop")
            step(core,"checking-exit-stop-revokes-core")
            control.request("POST",prefix+"/stop",expect=204)
            live.require(live.connection_refused(("127.0.0.1",port)),"encrypted entrance survived explicit exit stop")
            runtime=control.request("GET",prefix+"/ingress/runtime",expect=200)
            live.require(runtime["phase"]=="stopped","stopped exit retained active core state")
            observations.append({"core":core,"template":"vless-reality-vision","native_listener_owned":True,"business_https_verified":True,"exit_ip_matches_vpn":True,"exit_ipv4_sha256":hashlib.sha256(raw_ip.encode()).hexdigest(),"client_identity_survives_restart":True,"mutation_refused":True,"exit_stop_revokes_core":True,"listener_public":False})
        finally:client.close()
        control.request("DELETE",prefix+"/ingress",expect=204)
    # The shared public acceptance resumes its own probe/stop/delete lifecycle.
    control.request("POST",prefix+"/start",expect=200)
    return observations

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ("binary","data","sha256","xray","xray-sha256","sing-box","sing-box-sha256"):parser.add_argument("--"+name,required=True)
    parser.add_argument("--allow-public-vpngate",action="store_true");parser.add_argument("--allow-legacy-data",action="store_true");parser.add_argument("--allow-encrypted-ingress",action="store_true")
    args=parser.parse_args()
    live.require(sys.platform=="linux" and os.geteuid()==0 and args.allow_public_vpngate and args.allow_legacy_data and args.allow_encrypted_ingress,"authorized explicit Linux full-path test required")
    binary=checked_binary(args.binary,args.sha256)
    EncryptedControl.cores={"xray":checked_binary(args.xray,args.xray_sha256),"sing-box":checked_binary(args.sing_box,args.sing_box_sha256)}
    data=Path(args.data);info=data.lstat();live.require(data.resolve(strict=True)==data and info.st_uid==65534 and stat.S_IMODE(info.st_mode)==0o700 and not any(data.iterdir()),"fresh private test data required")
    group,limits=live.effective_limits();memory=live.counters(group/"memory.events");pids=live.counters(group/"pids.events");started=time.monotonic()
    result=public.exercise(binary,data,EncryptedControl,encrypted_path)
    result.update(source_binary_sha256=args.sha256,elapsed_seconds=round(time.monotonic()-started,3),limits=limits,memory_peak_bytes=int((group/"memory.peak").read_text()),memory_events_delta={k:v-memory[k] for k,v in live.counters(group/"memory.events").items()},pids_events_delta={k:v-pids[k] for k,v in live.counters(group/"pids.events").items()})
    result["remaining_scope_pids"]=[int(pid) for pid in (group/"cgroup.procs").read_text().split() if int(pid)!=os.getpid()]
    live.require(not result["remaining_scope_pids"] and not any(result["memory_events_delta"].values()) and not any(result["pids_events_delta"].values()),"test children/resource events remain")
    live.require(not result["success"] or len(result.get("encrypted_ingress",[]))==2,"success missing both real core paths")
    print("ENCRYPTED_VPN_RESULT "+json.dumps(result,sort_keys=True),flush=True)
    return 0 if result["success"] else 1
if __name__=="__main__":
    try:sys.exit(main())
    except Exception as exc:
        print("ENCRYPTED_VPN_FAILED "+type(exc).__name__+": "+str(exc),file=sys.stderr,flush=True);sys.exit(1)
