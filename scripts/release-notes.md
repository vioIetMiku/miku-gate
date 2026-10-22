## Miku Gate v0.2.4 正式版

基于 `miku-gate-v0.2.4.zip`，继续使用 **Miku Gate / miku-gate**。

### 主要更新

- 普通新建与快速连接按目标数量的 3 倍竞发，成功节点立即显示，达标后取消多余候选。
- 普通任务支持停止补齐和全部停止，候选不进入订阅、展示或恢复状态。
- OpenVPN 保活与连接重试参数集中管理，过滤冲突配置；软恢复保留 TUN 与密钥。
- 健康检查连续 3 次且持续 75 秒才重建，明确身份错误仍立即处理；新增 IP 来源回退。
- 继续提供 `miku` 独立安装文件、`mikugate.sh` 短入口、`miku quick` 和多格式订阅。

### 升级

```bash
miku backup
miku update /root/miku-gate-vps-v0.2.4.tar.gz
```

公开短入口保持：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/mikugate.sh)
```

公共 VPN Gate 节点的在线、速度和出口属性仍取决于志愿者节点及部署环境。
