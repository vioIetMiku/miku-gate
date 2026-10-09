# 在低资源 VPS 复核隔离实验

> 这不是生产安装脚本，也不会开放代理端口。只在已授权、可恢复的 Linux 测试机执行；不要照搬到业务服务器。测试仍会短暂创建网络命名空间、root 子进程和仅本机的 HTTP 夹具。现有 Web `start` 闸门不会因此解除。

## 先确认环境

需要 Linux、cgroup v2、systemd、TUN、root 的网络命名空间能力，以及 Python 3、OpenVPN 2.6、iproute2、nftables、util-linux、iputils-ping。构建在其他机器完成，不在小内存 VPS 安装 Go、Node 或编译工具链。

先核对主机身份与已有服务，再只读检查内存、挂载、路由、防火墙和监听。首次使用的 SSH 指纹固定只能提供后续连接连续性，不等于带外身份验证。不要在命令行、日志或 Issue 中填写 SSH 密码。

执行器要求：

- 可用内存至少 448MiB，`/run` 至少 48MiB 空余。
- 测试 scope 的 CPU 配额不超过一核、内存不超过 384MiB、swap 为零、任务数不超过 64；必须实际读回 cgroup 文件。
- 该 VPS 验收配方要求空的命名空间注册表，已知 Miku 测试目录不存在或为空；遇到旧资源不会擅自删除。
- root 私有产物目录必须是 `/var/tmp/miku-gate-lab-` 开头的规范路径、root 所有、0700，不允许符号链接。
- 三个测试二进制分别命名为 `netexec.test`、`executoripc.test`、`openvpn.test`，root 所有、0700、单硬链接；每次执行都核对预先取得的 SHA-256。
- 没有自动安装依赖、升级软件包、重启既有服务、重挂载文件系统或修改全局 swap 的步骤。

## `/run` 是 noexec 时怎么办

**保留 noexec，不要重挂载。** 运行时数据与可执行程序本来就不应绑定到同一个文件系统。

`internal/labexec` 只被 Linux 集成测试导入，不接入生产程序。它提供两个固定的测试可执行根：

| 设置 | 用途 | 限制 |
| --- | --- | --- |
| 未设置 / `/run/miku-gate-lab-exec` | 可执行的临时 `/run` 环境 | 检测到 noexec 时明确拒绝 |
| `MIKU_GATE_LAB_EXEC_ROOT=/usr/local/lib/miku-gate-lab-exec` | `/run` 为 noexec 的测试机 | 祖先必须为 root 所有、无组/其他用户写权限且可遍历；目标文件系统必须允许执行 |

不接受其他路径。只在新建的随机子目录中复制当前测试二进制，文件最终为 root 所有的 0555，子目录 0755。先完整写入并关闭写描述符，再向测试 UID 暴露执行权限。已存在但权限异常的根目录不会被 chmod 或接管。

测试用户 UID 65534 只能读/执行该副本，不能替换它。SQLite、Unix socket、namespace 所有权记录仍在原有 `/run` 路径；证书在内存中，OpenVPN 配置通过 sealed memfd 传递。清理只处理本次记录的 inode 和空目录，拒绝替换对象/多重硬链接，不使用递归删除。空的测试根目录可以保留以供检查。

## 构建并转移精确产物

在普通用户的构建机、干净源码提交上执行下面的 Linux amd64 交叉编译；目标机器不参与构建：

```bash
set -eu
mkdir -p .cache/vps-artifacts
SOURCE=$(git rev-parse HEAD)
test -z "$(git status --porcelain)"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -trimpath -ldflags='-s -w' \
  -o .cache/vps-artifacts/netexec.test ./internal/netexec
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -trimpath -ldflags='-s -w' \
  -o .cache/vps-artifacts/executoripc.test ./internal/executoripc
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -trimpath -ldflags='-s -w' \
  -o .cache/vps-artifacts/openvpn.test ./internal/tunnel
cp scripts/vps-lab-guard.py .cache/vps-artifacts/
printf '%s\n' "$SOURCE" > .cache/vps-artifacts/SOURCE
(cd .cache/vps-artifacts && sha256sum *.test vps-lab-guard.py > SHA256SUMS)
```

经已验证身份的 SSH/SFTP 通道转移到新的私有目录，不覆盖上次产物。由操作者先核对来源提交、校验文件与实际文件权限。使用自己在可信构建机取得的校验值；只对远端文件重新计算哈希而没有独立预期值，不能验证传输来源。

## 串行运行三组实验

在目标机，将 `LAB_DIRECTORY`、`SOURCE`、三个 `*_SHA256` 设为本次已核对的目录、完整提交与哈希。以下函数的测试命令和 suite 固定，调用失败后停止，不自动修复主机：

```bash
set -eu
: "${LAB_DIRECTORY:?set the verified root-private artifact directory}"
: "${SOURCE:?set the exact full source commit}"
: "${NETEXEC_SHA256:?set the trusted netexec.test SHA256}"
: "${IPC_SHA256:?set the trusted executoripc.test SHA256}"
: "${OPENVPN_SHA256:?set the trusted openvpn.test SHA256}"
run_lab() {
  systemd-run --quiet --scope --collect \
    --unit="miku-vps-$1-$(date +%s)-$$" \
    --property=CPUQuota=100% --property=MemoryMax=384M \
    --property=MemorySwapMax=0 --property=TasksMax=64 \
    --property=RuntimeMaxSec=240s \
    /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LANG=C LC_ALL=C \
      MIKU_GATE_VPS_LAB=1 GOMAXPROCS=1 GOMEMLIMIT=192MiB \
      /usr/bin/python3 -B "$LAB_DIRECTORY/vps-lab-guard.py" \
      --suite "$1" --directory "$LAB_DIRECTORY" --source "$SOURCE" --sha256 "$2"
}
run_lab netexec "$NETEXEC_SHA256"
run_lab ipc "$IPC_SHA256"
run_lab openvpn "$OPENVPN_SHA256"
```

守护脚本固定为测试子进程设置 `/usr/local/lib/miku-gate-lab-exec`，无需用户传入可执行路径或任意命令。每组使用独立 scope，不并行。只有准确命中 7、4、1 项主测试、无 SKIP、退出码 0、宿主快照一致且没有残留/资源拒绝时，才输出 `success: true`。netexec 组还包含 6 个文件对象子用例。

## 如何读结果

每组保留一个时间戳 run ID，以及独占新建的 `.before.json`、`.after.json`、`.log`、`.json`。第一次生成的 `host-baseline.json` 在后续组复用，防止只对比每组自己而漏掉跨组变化。所有文件仅 root 可读写。

前后比较包括接口、IPv4/IPv6 路由与策略规则、nftables、DNS 文件哈希、转发/IPv6 开关、监听端点和 PID 起始时间、命名空间注册表，以及 `/run` 和可执行根祖先的挂载选项。只忽略路由倒计时、nft 字节/包计数和 socket 队列长度，不忽略策略变化。

- `memory_peak_bytes`：该 scope 的 `memory.peak`，包含 Python 守护、测试进程、子进程、内核记账及相关文件缓存，不是网关常驻内存。
- `child_maxrss_kib`：`RUSAGE_CHILDREN` 的最大子进程 RSS，不是全部进程之和，不能与 scope 峰值混用。
- `elapsed_seconds`：本轮测试进程的墙钟时间，不是吞吐、延迟或长稳容量测试。
- `memory_events_delta`、`pids_events_delta`：不得出现内存压力/OOM/任务配额拒绝增量。
- 输出只包含宿主快照哈希；原始拓扑留在 root 私有目录，不应直接上传到 Issue。

脚本限制测试输出 2MiB、测试进程总等待约 200 秒，并限定预先批准的二进制/测试正则。遇到进程失败只终止自身创建的会话；遇到遗留 namespace 或文件仅保留证据并报错，不猜测归属或批量清理。

## 本机回归与验收边界

无 root、无网络的守护逻辑测试：

```bash
python3 -B -m unittest discover -s scripts -p test_vps_lab_guard.py -v
```

Go 的固定路径/noexec/清理拒绝测试纳入 Linux `go test ./...`；实际跨 UID 执行由上面的 IPC 集成实验覆盖。普通 Windows Go 测试与交叉编译都不能替代它。

这些实验不连接公共 VPN 节点，也不测住宅属性、真实公网出口 IP、生产代理入口、长时间负载或自动重连。通过后仍需完成生产权限分离、受限生命周期、worker 连接交接和代理入口接线；不能只把 HTTP 409 改成 200。
