# Miyabi

配合 115 网盘的一站式 Jav 管理平台。

## 项目预览

![媒体库](./screenshots/ScreenShot_2026-09-19_131853_357.png)
![播放器](./screenshots/ScreenShot_2026-09-19_131853_237.png)
![字幕](./screenshots/ScreenShot_2026-09-23_131948_796.png)
![发现](./screenshots/ScreenShot_2026-09-19_132213_340.png)
![搜索](./screenshots/ScreenShot_2026-09-19_132227_063.png)
![设置](./screenshots/ScreenShot_2026-09-19_132249_331.png)

## Docker 部署

> \[!TIP]
> 您可以借助 AI 完成项目的部署。

公开镜像：

```text
ghcr.io/ppxb/miyabi
```

直接使用 Docker 运行（请替换访问密码）：

```bash
docker run -d \
  --name miyabi \
  --restart unless-stopped \
  --security-opt no-new-privileges:true \
  -p 8080:8080 \
  -v miyabi-data:/app/data \
  -e MIYABI_ACCESS_PASSWORD='change-this-password' \
  ghcr.io/ppxb/miyabi:latest
```

启动后访问 `http://<服务器IP>:8080`。

查看日志：

```bash
docker logs -f miyabi
```

升级时先拉取新镜像，再停止并移除旧容器：

```bash
docker pull ghcr.io/ppxb/miyabi:latest
docker stop miyabi
docker rm miyabi
```

然后重新执行上方的 `docker run` 命令，沿用原访问密码和 `miyabi-data` 数据卷。

## 启动配置

启动配置统一使用环境变量，未设置时采用默认值。监听地址、数据目录和日志级别显式设为空时会报错。

通过 `MIYABI_ACCESS_PASSWORD` 配置访问密码。未配置密码或将密码设为空时关闭门禁。**强烈建议您开启门禁**。

| 环境变量                 | 用途                  | 默认值                                |
| ------------------------ | --------------------- | ------------------------------------- |
| `MIYABI_LISTEN`          | HTTP 监听地址         | `:8080`                               |
| `MIYABI_DATA_DIR`        | SQLite 与图片缓存目录 | 二进制为 `./data`，容器为 `/app/data` |
| `MIYABI_LOG_LEVEL`       | 日志级别              | `info`                                |
| `MIYABI_ACCESS_PASSWORD` | Web 入口密码          | 空，关闭门禁                          |

## NSFW 警告

本软件可能存在裸露、暴力、色情或冒犯等不适宜公众场合的内容，请勿在公共场合使用本软件，避免不必要的纷争。

## 特别鸣谢

感谢 [莫愁](https://github.com/zk020106) 的 token，感谢 [老罗](https://github.com/luoqiz) 的115账号。

## 致谢

本项目参考了以下项目的部分实现，在此表示衷心的感谢！

- [javdb-cli](https://github.com/FlanChanXwO/javdb-cli)

同时感谢社区 [LinuxDO](https://linux.do) 的帮助。

## 免责声明

本项目仅供学习、研究和技术交流使用。项目作者与任何第三方服务、原始应用或内容提供方无关。
使用者应自行遵守当地法律法规以及相关服务条款。因使用本项目产生的任何法律、版权、账号、数据或财务风险均由使用者自行承担。

## License

遵循 [GNU GPL v3](./LICENSE) 协议。
