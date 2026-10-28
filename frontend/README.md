# 独立前端

`public/` 是此分离版前端的维护入口：HTML、CSS、JavaScript 和图片可独立发布，浏览器请求使用相对路径，通过 Nginx 代理到 Go 后端。无需 Node.js，也无需重编译后端。

- `nginx.conf.template`：同源代理、会话检查和静态资源路由。
- `ASSETS.json`：资源 SHA256 清单。
- `.runtime/`：启动器生成的端口记录与 Nginx 配置，不打包、不提交、不复制到其他部署。

修改后，在项目根目录执行：

```bash
python3 -B scripts/export-frontend.py --refresh-manifest
sudo bash install-docker.sh frontend restart
sudo bash install-docker.sh frontend info
```

`python3 -B scripts/export-frontend.py` 默认只校验资源。`--from-backend` 会主动用 Go 内嵌兼容页面覆盖独立前端（包括本版的 Docker 专用提示），仅在确定需要重新导入时使用。发布脚本不会自动重新导入。

完整安装、另一台服务器部署和 HTTPS 配置见 [FRONTEND.md](../docs/FRONTEND.md)。
