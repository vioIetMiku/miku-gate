# VPN Gate 反代 Worker

fanout 拉节点列表时直连失败的兜底。部署在 Cloudflare Workers 上，
只转发 VPN Gate 的节点列表接口，不是通用代理。

官方部署在 `https://p.xy.kg/vpngate`，fanout 默认就用它，不需要自己搭。
想换成自己的，按下面来。

## 自己部署

1. Cloudflare 控制台建一个 Worker，把 `worker.js` 的内容贴进去。
2. 在 Worker 的 Settings - Variables 里加一个 Secret，名字 `ACCESS_KEY`，值随便一串随机字符。
3. 绑一个自己的域名。
4. 给 fanout 设两个环境变量：

```
FANOUT_VPNGATE_MIRROR=https://你的域名/vpngate
FANOUT_VPNGATE_MIRROR_KEY=你刚才设的 ACCESS_KEY
```

`FANOUT_VPNGATE_MIRROR` 设成空字符串就是彻底关掉兜底，只走直连。

## 关于那个 key

它只是让爬虫和端口扫描器扫到这个域名时看到 404，别把 Worker 当免费流量白嫖。
fanout 是开源的，密钥就写在源码里，所以这不是安全措施，别指望它挡住有心人。
