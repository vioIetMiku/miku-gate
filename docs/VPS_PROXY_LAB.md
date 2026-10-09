# 实际 CLI 代理路径的 VPS 验收

> 只在授权测试机显式运行，不是安装脚本，也不是OpenVPN/住宅出口验收。该实验会向Cloudflare及Google的公开DNS/TLS端点发出少量正常请求。需要明确网络许可，不能隐式运行于普通单元测试。

脚本 `scripts/vps-proxy-live.py` 启动的是正式CLI和默认上游工厂，不注入替代拨号器、证书信任根或HTTP响应。临时上游只监听127.0.0.1，需要随机认证，且只允许`1.1.1.1:443`、`8.8.8.8:443`、`8.8.4.4:443`三个数字端点。

## 验证范围

1. CLI init→serve、单数据目录锁、运行时只读instance查询。
2. 加密保存上游、生成的入口凭据、真实启动预检、按实际监听返回状态。
3. SOCKS上游承载Cloudflare DoH；HTTP CONNECT入口访问真实TLS的`1.1.1.1/cdn-cgi/trace`，SOCKS域名入口访问`dns.google/resolve`。TLS保持系统根校验。
4. 运行中配置/凭据/删除互锁；关闭上游后已建立连接关闭、新连接不能绕行、健康降级。
5. 停止后入口不可连接；切到HTTP CONNECT上游使用同一入口和凭据。
6. 杀死控制进程后入口关闭；重新启动不伪造活动状态，密钥与上游配置保留；再次显式启动能继续传输。
7. 实际cgroup资源配额、内存/任务拒绝计数与子进程残留。

**数据边界：** 此处上游是测试机上的受限环回中继，使用的仍是VPS公网出口，不是不同机器的出口、更不是公共VPN Gate。trace响应只确认实际经过选定链路取得一个公网IPv4，发布日志仅保留其哈希，不将其当作住宅或信誉证明。

## 启动条件

- Linux非root测试UID、Python3、系统CA证书和已编译的miku-gate；不在VPS编译。
- 由管理员先核对服务器身份、现有服务与网络快照；没有修改路由/防火墙/全局swap或接管已有节点服务的步骤。
- 将精确来源二进制和脚本放进已核对的root所有可执行目录，二进制0555、单硬链接。`/run`的noexec应保留。
- 数据目录全新、实际测试UID所有、0700；测试前检查至少448MiB可用内存。
- 指定独立的`miku-proxy-live-*.scope`，CPUQuota=100%、MemoryMax=384M、MemorySwapMax=0、TasksMax=64；脚本逐项读回，不仅相信命令行。

由管理员预先设置已核验的`LAB_BINARY`、`LAB_SCRIPT`、`LAB_DATA`、`LAB_SHA256`环境变量后，按单任务串行执行：

```bash
set -eu
: "${LAB_BINARY:?absolute root-owned binary path}"
: "${LAB_SCRIPT:?absolute reviewed script path}"
: "${LAB_DATA:?new private directory owned by UID 65534}"
: "${LAB_SHA256:?trusted binary SHA256 from build host}"
systemd-run --quiet --scope --collect \
  --unit="miku-proxy-live-$(date +%s)-$$" \
  --property=CPUQuota=100% --property=MemoryMax=384M \
  --property=MemorySwapMax=0 --property=TasksMax=64 --property=RuntimeMaxSec=240s \
  /usr/bin/setpriv --reuid=65534 --regid=65534 --clear-groups --no-new-privs \
  /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LANG=C.UTF-8 LC_ALL=C.UTF-8 \
  /usr/bin/python3 -B "$LAB_SCRIPT" --binary "$LAB_BINARY" \
    --data "$LAB_DATA" --sha256 "$LAB_SHA256" --allow-network
```

65534只是受控测试UID，不是生产服务身份。实际产品应使用专属账号；不要为试验修改已有nobody账号的配置。首次账号和代理密码只留在测试进程内存，不输出到日志；数据库中的上游材料加密保存。

测试成功必须看到完整`LIVE_PROXY_RESULT`且`success=true`；HTTP或TLS错误不能算通过。退出后由管理员对比宿主接口/路由/防火墙/DNS/现有监听进程和挂载策略，核对scope不存在。只清理本次已记录inode及空目录，不递归删除未知路径；原始私有证据留存。

普通本机离线安全回归：

```bash
python3 -B -m unittest discover -s scripts -p 'test_vps_*.py' -v
```
