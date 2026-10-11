import importlib.util
import io
import json
from pathlib import Path
import tarfile
import unittest

spec=importlib.util.spec_from_file_location('package_dist',Path(__file__).with_name('package-dist.py'))
package=importlib.util.module_from_spec(spec);spec.loader.exec_module(package)
spec=importlib.util.spec_from_file_location('package_install',Path(__file__).with_name('miku-install.py'))
install=importlib.util.module_from_spec(spec);spec.loader.exec_module(install)

class DistributionConsistency(unittest.TestCase):
    def metadata(self):
        return {'format':1,'source':'a'*40,'version':'v0.1.0-dev.11','platform':'linux-amd64','binary_sha256':'b'*64,'binary_bytes':8,'script_sha256':{name:'c'*64 for name in install.DISTRIBUTION_SCRIPTS}}
    def test_platform_metadata_and_complete_script_pairing(self):
        raw=json.dumps(self.metadata()).encode();self.assertEqual(install.release_metadata(raw,'amd64')['source'],'a'*40)
        for field,value in [('platform','linux-arm64'),('source','latest'),('binary_sha256','x'*64),('script_sha256',{'miku-install.py':'c'*64}),('version','../../unsafe'),('format',0)]:
            meta=self.metadata();meta[field]=value
            with self.assertRaises(install.InstallError):install.release_metadata(json.dumps(meta).encode(),'amd64')
    def test_embedded_exact_clean_static_source_required(self):
        text='\n'.join('  build  '+line for line in ['vcs.revision='+'a'*40,'vcs.modified=false','GOOS=linux','GOARCH=amd64','CGO_ENABLED=0'])
        package.validate_build(text,'a'*40,'linux-amd64')
        for changed in (text.replace('modified=false','modified=true'),text.replace('CGO_ENABLED=0','CGO_ENABLED=1'),text.replace('GOARCH=amd64','GOARCH=arm64')):
            with self.assertRaises(package.PackageError):package.validate_build(changed,'a'*40,'linux-amd64')
    def test_reproducible_bounded_archive_without_extraction(self):
        files={'bin/miku-gate':(b'fixture-binary',0o755),'BUILD.json':(b'{}',0o644),'scripts/install-vps.sh':(b'#!/bin/sh\n',0o644)}
        raw=package.archive_bytes('miku-fixture',files,'linux-amd64',100)
        self.assertEqual(raw,package.archive_bytes('miku-fixture',files,'linux-amd64',100))
        with tarfile.open(fileobj=io.BytesIO(raw),mode='r:gz') as archive:
            self.assertEqual(len(archive.getmembers()),3);self.assertEqual(archive.getmember('miku-fixture/bin/miku-gate').mode,0o755)
        with self.assertRaises(package.PackageError):package.archive_bytes('miku-fixture',{'../escape':(b'x',0o644)},'linux-amd64',100)

if __name__=='__main__':unittest.main()
