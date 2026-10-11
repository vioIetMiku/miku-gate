#!/usr/bin/env python3
"""Package existing clean-source binaries offline; never infer or download latest.

Go's embedded build information must match clean HEAD and the requested target.
The archive contains only HEAD's selected tracked docs/coordinators and a binary.
"""
import argparse
import gzip
import hashlib
import io
import json
from pathlib import Path
import re
import subprocess
import tarfile
import zipfile

ROOT=Path(__file__).resolve().parent.parent
SCRIPTS=('install-vps.sh','miku-install.py','miku-maintain.py')
DOCS=('README.md','CHANGELOG.md','docs/HTTPS_INSTALLATION.md','docs/CONFIDENTIAL_BACKUP.md','docs/DISTRIBUTION.md','docs/UPGRADE_ROLLBACK.md','docs/CERTIFICATE_RENEWAL.md','docs/IP_INTELLIGENCE.md','docs/PANEL_REUSE.md','docs/UNINSTALL.md','docs/AUTO_RECOVERY.md','docs/FINAL_FEATURES.md','docs/DELIVERY_PLAN.md')
MAX_BINARY=48<<20
MAX_PAYLOAD=64<<20

class PackageError(RuntimeError):pass

def require(value,message):
    if not value:raise PackageError(message)

def digest(raw):return hashlib.sha256(raw).hexdigest()

def command(args):
    result=subprocess.run(args,cwd=ROOT,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
    require(result.returncode==0,'packaging subprocess failed: '+Path(args[0]).name)
    require(len(result.stdout)<=2<<20,'packaging subprocess output exceeded bound')
    return result.stdout

def validate_build(text,source,platform):
    settings={}
    for line in text.splitlines():
        parts=line.strip().split(None,1)
        if len(parts)==2 and parts[0]=='build' and '=' in parts[1]:
            key,value=parts[1].split('=',1);settings[key]=value.strip('"')
    require(settings.get('vcs.revision')==source and settings.get('vcs.modified')=='false','binary does not belong to clean exact source commit')
    require(settings.get('GOOS','')+'-'+settings.get('GOARCH','')==platform and settings.get('CGO_ENABLED')=='0','binary target/static build mismatch')
    return settings

def archive_bytes(prefix,files,platform,stamp):
    require(all(re.fullmatch(r'[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*',name) and '..' not in name.split('/') for name in files),'unsafe distribution member')
    require(sum(len(raw) for raw,_ in files.values())<=MAX_PAYLOAD,'distribution payload exceeds budget')
    output=io.BytesIO()
    if platform.startswith('windows-'):
        with zipfile.ZipFile(output,'w',compression=zipfile.ZIP_DEFLATED) as archive:
            for name,(raw,mode) in sorted(files.items()):
                item=zipfile.ZipInfo(prefix+'/'+name,(2026,10,11,0,0,0));item.create_system=3;item.external_attr=(0o100000|mode)<<16;item.compress_type=zipfile.ZIP_DEFLATED
                archive.writestr(item,raw)
        with zipfile.ZipFile(io.BytesIO(output.getvalue())) as archive:
            require(len(archive.infolist())==len(files),'zip distribution member count mismatch')
            for name,(raw,_) in files.items():require(archive.read(prefix+'/'+name)==raw,'zip distribution content mismatch')
    else:
        with gzip.GzipFile(fileobj=output,mode='wb',filename='',mtime=stamp) as zipped:
            with tarfile.open(fileobj=zipped,mode='w',format=tarfile.USTAR_FORMAT) as archive:
                for name,(raw,mode) in sorted(files.items()):
                    item=tarfile.TarInfo(prefix+'/'+name);item.size=len(raw);item.mode=mode;item.uid=item.gid=0;item.mtime=stamp
                    archive.addfile(item,io.BytesIO(raw))
        with tarfile.open(fileobj=io.BytesIO(output.getvalue()),mode='r:gz') as archive:
            require(len(archive.getmembers())==len(files),'tar distribution member count mismatch')
            for name,(raw,mode) in files.items():
                item=archive.getmember(prefix+'/'+name)
                require(item.isfile() and item.mode==mode and archive.extractfile(item).read()==raw,'tar distribution content/permissions mismatch')
    return output.getvalue()

def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary',required=True);parser.add_argument('--version',required=True)
    parser.add_argument('--platform',choices=('linux-amd64','linux-arm64','windows-amd64'),required=True)
    parser.add_argument('--go',default='go');parser.add_argument('--output-dir',required=True)
    args=parser.parse_args(argv)
    require(re.fullmatch(r'v0\.[0-9]+\.[0-9]+(?:-dev\.[0-9]+)?',args.version) is not None,'invalid preview version')
    require(not command(['git','status','--porcelain']).strip(),'source must be clean before packaging')
    source=command(['git','rev-parse','HEAD']).decode().strip()
    version=command(['git','show',source+':internal/domain/domain.go']).decode('utf-8')
    require('const Version = "'+args.version[1:]+'"' in version,'requested package version disagrees with source')
    require(not Path(args.binary).is_symlink(),'binary must not be a symlink')
    binary=Path(args.binary).resolve(strict=True)
    require(binary.is_file() and not binary.is_symlink() and binary.stat().st_size<=MAX_BINARY,'binary bound/type mismatch')
    info=command([args.go,'version','-m',str(binary)]).decode('utf-8')
    validate_build(info,source,args.platform)
    files={}
    for name in (*DOCS,*('scripts/'+s for s in SCRIPTS)):
        raw=command(['git','show',source+':'+name]);files[name]=(raw,0o644)
    raw=binary.read_bytes();executable='miku-gate.exe' if args.platform.startswith('windows-') else 'miku-gate'
    files['bin/'+executable]=(raw,0o755)
    meta={'format':1,'version':args.version,'source':source,'platform':args.platform,'binary_sha256':digest(raw),'binary_bytes':len(raw),'script_sha256':{s:digest(files['scripts/'+s][0]) for s in SCRIPTS},'trust':'Verify the archive SHA256 from the authenticated release before root execution; BUILD.json is internal consistency, not a signature.'}
    files['BUILD.json']=(json.dumps(meta,ensure_ascii=False,sort_keys=True,indent=2).encode()+b'\n',0o644)
    stamp=int(command(['git','show','-s','--format=%ct',source]))
    prefix='miku-gate-'+args.version+'-'+args.platform
    archive=archive_bytes(prefix,files,args.platform,stamp)
    suffix='.zip' if args.platform.startswith('windows-') else '.tar.gz'
    directory=Path(args.output_dir).resolve();directory.mkdir(parents=True,exist_ok=True)
    destination=directory/(prefix+suffix)
    with destination.open('xb') as output:output.write(archive)
    summary={'name':destination.name,'source':source,'version':args.version,'platform':args.platform,'bytes':len(archive),'sha256':digest(archive),'binary_sha256':meta['binary_sha256'],'member_count':len(files),'readback_verified':True}
    with destination.with_suffix(destination.suffix+'.json').open('x',encoding='utf-8') as output:json.dump(summary,output,sort_keys=True,indent=2)
    print(json.dumps(summary,sort_keys=True))

if __name__=='__main__':
    try:main()
    except (PackageError,OSError,ValueError,subprocess.TimeoutExpired) as exc:
        raise SystemExit('PACKAGE_STOPPED '+type(exc).__name__+': '+str(exc))
