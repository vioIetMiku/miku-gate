#!/usr/bin/env python3
"""Build universal Linux VPS bundle and source archive. Never runs a gateway."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import tarfile
import tempfile
import zipfile

ROOT=Path(__file__).resolve().parents[1]
UPSTREAM='d7ce5224caf3469b19876982abdf8d1be16c4a8b'

def main():
    parser=argparse.ArgumentParser();parser.add_argument('--version',default='0.2.0');parser.add_argument('--label',default='v0.2.0');parser.add_argument('--output',type=Path,default=ROOT/'dist');args=parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9_.-]+',args.version) or not re.fullmatch(r'[A-Za-z0-9_.-]+',args.label):raise ValueError('invalid version/label')
    args.output=args.output.resolve()
    args.output.mkdir(parents=True,exist_ok=True)
    revision=subprocess.run(['git','rev-parse','HEAD'],cwd=ROOT,text=True,capture_output=True)
    if revision.returncode==0:
        source=revision.stdout.strip()
    else:
        # Source archives have no .git directory. Record their source bytes.
        digest=hashlib.sha256()
        for path in sorted(ROOT.rglob('*')):
            relative=path.relative_to(ROOT)
            if not path.is_file() or any(part in ('.git','bin','dist','__pycache__') for part in relative.parts) or path.suffix in ('.zip','.gz','.pyc') or relative.as_posix()=='BUILD.json':continue
            digest.update(relative.as_posix().encode()+b'\0'+path.read_bytes()+b'\0')
        source='archive-sha256:'+digest.hexdigest()
    with tempfile.TemporaryDirectory(prefix='miku-release-',dir=args.output) as temporary:
        stage=Path(temporary)/'miku-fanout';(stage/'bin').mkdir(parents=True);(stage/'scripts').mkdir()
        for name in ('install.sh','install-online.sh','VERSION','CHANGELOG.md','README.md','README.fanout.md','PROVENANCE.md','ARTWORK.md','UI_DESIGN.md','LICENSE','CORE_ASSETS.json'):
            shutil.copy2(ROOT/name,stage/name)
        for name in ('common.py','install.py','maintain.py','miku.sh'):
            shutil.copy2(ROOT/'scripts'/name,stage/'scripts'/name)
        (stage/'docs').mkdir()
        shutil.copyfile(ROOT/'docs/DEV_HISTORY.md',stage/'docs/DEV_HISTORY.md')
        (stage/'assets').mkdir()
        shutil.copyfile(ROOT/'assets/miku-hero-v1.png',stage/'assets/miku-hero-v1.png')
        def build(architecture,helper):
            environment={**os.environ,'GOOS':'linux','GOARCH':architecture,'CGO_ENABLED':'0','GOTELEMETRY':'off'}
            name=('miku-export' if helper else 'miku')+'-linux-'+architecture
            directory=ROOT/'compat'/'miku-export' if helper else ROOT
            flags='-s -w'+('' if helper else ' -X main.version='+args.version)
            subprocess.run(['go','build','-buildvcs=false','-trimpath','-ldflags',flags,'-o',str(stage/'bin'/name),'./cmd/miku-export' if helper else '.'],cwd=directory,env=environment,check=True)
            print('Built '+name,flush=True)
        with ThreadPoolExecutor(max_workers=4) as pool:
            list(pool.map(lambda pair:build(*pair),[(architecture,helper) for architecture in ('amd64','arm64') for helper in (False,True)]))
        files={path.relative_to(stage).as_posix():hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted(stage.rglob('*')) if path.is_file()}
        meta={'product':'miku-fanout','format':1,'version':args.version,'source':source,'upstream':UPSTREAM,'architectures':['amd64','arm64'],'files':files}
        (stage/'BUILD.json').write_text(json.dumps(meta,indent=2)+'\n')
        for name in [*files,'BUILD.json']:
            path=stage/name;path.chmod(0o755 if path.parent.name=='bin' or path.suffix=='.sh' else 0o644)
        target=args.output/('miku-gate-vps-'+args.label+'.tar.gz')
        compressed=Path(temporary)/'release.tar.gz'
        members=['miku-fanout/'+name for name in sorted(set(files)|{'BUILD.json'})]
        # Finish the tar first, then wait for gzip to exit and sync its output.
        # This also avoids buffered compressor writes during stage cleanup.
        uncompressed=Path(temporary)/'release.tar'
        with tarfile.open(uncompressed,'w') as archive:
            for member in members:
                path=stage.parent/member
                info=archive.gettarinfo(str(path),arcname=member)
                info.uid=info.gid=0;info.uname=info.gname='';info.mode=0o755 if path.parent.name=='bin' or path.suffix=='.sh' else 0o644
                with path.open('rb') as contents:archive.addfile(info,contents)
        with compressed.open('wb') as complete:
            import gzip
            with uncompressed.open('rb') as source_tar, gzip.GzipFile(filename='',mode='wb',compresslevel=6,fileobj=complete,mtime=0) as compressor:
                shutil.copyfileobj(source_tar,compressor)
            complete.flush()
            os.fsync(complete.fileno())
        # Read through gzip's footer and all tar members before announcing it.
        import gzip
        with gzip.open(compressed,'rb') as checked:
            while checked.read(1<<20):pass
        with tarfile.open(compressed,'r:gz') as checked:
            expected=set(files)|{'BUILD.json'}
            actual=set()
            for item in checked:
                name=item.name.removeprefix('miku-fanout/')
                if not item.isfile() or item.mode&0o022 or item.uid!=0:raise RuntimeError('archive file permissions invalid')
                actual.add(name)
                if name in files and hashlib.sha256(checked.extractfile(item).read()).hexdigest()!=files[name]:raise RuntimeError('archive checksum mismatch')
            if actual!=expected:raise RuntimeError('archive contents incomplete')
        # Publish a fresh, verified inode instead of overwriting an archive.
        os.replace(compressed,target)
        sources=args.output/('miku-gate-'+args.label+'.zip')
        source_stage=Path(temporary)/'source'/'miku-fanout'
        source_stage.mkdir(parents=True)
        source_files=[]
        for path in sorted(ROOT.rglob('*')):
            relative=path.relative_to(ROOT)
            if not path.is_file() or any(part in ('.git','bin','dist','__pycache__') for part in relative.parts) or path.suffix in ('.zip','.gz','.pyc') or relative.as_posix()=='BUILD.json':continue
            destination=source_stage/relative;destination.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(path,destination)
            destination.chmod(0o755 if path.suffix=='.sh' else 0o644);source_files.append(relative.as_posix())
        (source_stage/'bin').mkdir()
        for path in sorted((stage/'bin').iterdir()):
            shutil.copyfile(path,source_stage/'bin'/path.name);(source_stage/'bin'/path.name).chmod(0o755);source_files.append('bin/'+path.name)
        shutil.copyfile(stage/'BUILD.json',source_stage/'BUILD.json');source_files.append('BUILD.json')
        if sources.exists():sources.unlink()
        with zipfile.ZipFile(sources,'w',compression=zipfile.ZIP_DEFLATED,compresslevel=6) as archive:
            for name in source_files:
                info=zipfile.ZipInfo('miku-fanout/'+name,date_time=(2026,10,14,0,0,0))
                info.create_system=3
                mode=0o755 if name.startswith('bin/') or name.endswith('.sh') else 0o644
                info.external_attr=(stat.S_IFREG|mode)<<16
                archive.writestr(info,(source_stage/name).read_bytes(),compress_type=zipfile.ZIP_DEFLATED,compresslevel=6)
        with sources.open('rb') as complete:
            os.fsync(complete.fileno())
        with zipfile.ZipFile(sources) as checked:
            if checked.testzip() is not None:raise RuntimeError('source archive CRC failed')
            if set(checked.namelist())!={'miku-fanout/'+name for name in source_files}:raise RuntimeError('source archive incomplete')
        bootstrap=args.output/'install-online.sh'
        shutil.copyfile(ROOT/'install-online.sh',bootstrap)
        checksums=args.output/'SHA256SUMS'
        checksums.write_text(''.join(hashlib.sha256(path.read_bytes()).hexdigest()+'  '+path.name+'\n' for path in (target,sources,bootstrap)))
        print(target);print(sources)

if __name__=='__main__':main()
