#!/usr/bin/env bash
# Run only in an ephemeral Linux CI runner, from an unpacked release bundle.
set -euo pipefail
export WEB_PORT=38899 MIKU_PUBLIC_IP=1.1.1.1
sudo modprobe tun
[[ -c /dev/net/tun ]]
docker compose config --quiet
bash install-docker.sh build
cleanup() {
  status=$?
  docker compose logs --tail=80 miku || true
  docker compose down || true
  exit "$status"
}
trap cleanup EXIT
# Capture info (contains an ephemeral test password) without printing credentials.
cached_start=$(bash install-docker.sh up)
[[ "$cached_start" == *'复用本地镜像'* ]]
unset cached_start
echo 'PASS: unchanged-input Docker image reused without rebuilding.'
docker compose exec -T miku miku status
# Confirm this VPS-oriented container really has mount / setns permissions.
docker compose exec -T miku sh -ec 'ns="miku-ci-$$"; ip netns add "$ns"; trap '\''ip netns delete "$ns"'\'' EXIT; ip netns exec "$ns" ip link set lo up'
docker compose exec -T miku sh -ec 'printf "%s\n" "fixture node state" > /var/lib/miku/docker-ci-state'
before=$(docker compose exec -T miku sha256sum /var/lib/miku/password /var/lib/miku/basepath /var/lib/miku/docker-ci-state)
bash install-docker.sh backup
# backup must resume the previously running service.
docker compose up -d --wait --wait-timeout 180
docker compose exec -T miku miku status
docker compose stop miku
docker compose up -d --force-recreate --wait --wait-timeout 180
after=$(docker compose exec -T miku sha256sum /var/lib/miku/password /var/lib/miku/basepath /var/lib/miku/docker-ci-state)
[[ "$before" == "$after" ]]
# Restore our trusted, locally generated backup using a throwaway directory,
# never traversing archive paths or modifying host certificates.
docker compose stop miku
sudo python3 -B - <<'PY'
from pathlib import Path
import hashlib
import shutil
import tarfile
import tempfile
root = Path.cwd()
backups = sorted((root/'docker-backups').glob('miku-docker-*.tar.gz'))
assert len(backups) == 1
assert backups[0].stat().st_mode & 0o777 == 0o600
with tarfile.open(backups[0], 'r:gz') as archive:
    for name in ('password', 'basepath', 'docker-ci-state'):
        member = archive.getmember('data/' + name)
        assert member.isfile()
        raw = archive.extractfile(member).read()
        assert raw == (root/'docker-data'/name).read_bytes()
    raw = archive.extractfile('data/docker-ci-state').read()
    with tempfile.TemporaryDirectory(prefix='miku-ci-restore-') as staging:
        recovered = Path(staging)/'docker-ci-state'
        recovered.write_bytes(raw)
        (root/'docker-data/docker-ci-state').write_bytes(b'changed fixture')
        shutil.copyfile(recovered, root/'docker-data/docker-ci-state')
    print('Docker backup contents and fixture restoration verified; credentials not printed.')
PY
# A backup made while stopped must not start the main service.
bash install-docker.sh backup
[[ -z "$(docker compose ps --status running -q miku)" ]]
docker compose up -d --wait --wait-timeout 180
[[ "$before" == "$(docker compose exec -T miku sha256sum /var/lib/miku/password /var/lib/miku/basepath /var/lib/miku/docker-ci-state)" ]]
docker compose down
sudo test -s docker-data/password
# Also validate the Git-checkout build path, not only release/prebuilt mode.
docker build --build-arg MIKU_BINARY_SOURCE=source -t miku-gate:ci-source -f "$GITHUB_WORKSPACE/Dockerfile" "$GITHUB_WORKSPACE"
[[ "$(docker run --rm --entrypoint /usr/local/lib/miku/miku miku-gate:ci-source -version)" == "Miku Gate $(cat VERSION)" ]]
echo 'PASS: Docker build, health, netns, backup/fixture restore, stopped-state preservation, recreation persistence, source build.'
