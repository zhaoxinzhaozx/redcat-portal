# 站点门户

这是挂在 [Sub2API](https://github.com/Wei-Shaw/sub2api) 旁边的一个独立小服务。它提供使用教程页和抽奖活动页，主程序仍然使用官方镜像，版本更新时不用再合并这两页的改动。

门户只在自己的数据库模式里建表，模式名由 `DB_SCHEMA` 决定。它不修改用户表、订单表和余额表。充值资格只读取已完成的充值订单。中奖用户领取时，通过主站官方的管理员余额接口入账。

## 功能

- 使用教程：注册登录、创建密钥、下载客户端、用 CC Switch 导入。下载前会先在按钮下面确认。
- 下载按钮的文字和完整地址写在配置里，不写死在页面中。
- 抽奖活动：管理员设置开始、结束、公布时间、奖励金额、充值门槛和中奖人数。
- 一场活动可以有多个中奖人。管理员可以从真实参与者里勾选；没选满的名额，到公布时间从其余参与者中随机补足。
- 公布后，普通用户只能看到打码邮箱，例如 `adm******com`。管理员在后台仍能看到完整邮箱。
- 中奖用户点「立即领取」后，奖励进入主站账户余额，不能重复领取。

## 配置

复制 `deploy/portal.env.example` 为 `deploy/portal.env`。这个文件含数据库密码和管理员密码，不要提交到 Git。

| 变量 | 作用 |
| --- | --- |
| `SITE_NAME` | 页面标题、页眉和教程里的站点名称 |
| `SITE_SLOGAN` | 首页口号 |
| `SUPPORT_CONTACT` | 客服联系方式。留空则不显示联系方式 |
| `SITE_URL` | 教程里展示的接口地址 |
| `DOWNLOADS` | 下载按钮。多项用 `;;` 分开，每一项是 `步骤\|按钮文字\|确认提示\|完整地址` |
| `BASE_PATH` | 门户挂在主站上的路径，默认 `/portal` |
| `DB_SCHEMA` | 门户自己的数据库模式名，默认 `portal` |
| `CLAIM_KEY_PREFIX` | 领奖幂等键前缀，默认 `portal-claim` |
| `POSTGRES_*` | 与主站同一个数据库。容器内主机名一般是 `postgres` |
| `SUB2API_BASE_URL` | 主站容器地址，默认 `http://sub2api:8080` |
| `PORTAL_ADMIN_EMAIL` / `PORTAL_ADMIN_PASSWORD` | 用来调用主站入账接口的管理员。这个账号不能开启二次验证 |

`DOWNLOADS` 里的地址必须是完整的 `http://` 或 `https://` 链接。第 5 步和第 6 步只显示对应步骤的按钮。

Compose 还会读取同目录的 `.env`，用来指定镜像名、容器名、配置文件名和要加入的 Docker 网络：

```
PORTAL_IMAGE=portal:local
PORTAL_CONTAINER_NAME=portal
PORTAL_ENV_FILE=portal.env
PORTAL_DOCKER_NETWORK=sub2api-network
```

`.env` 同样不要提交。

## 部署

在本目录构建镜像：

```bash
docker build -t portal:local .
```

把项目目录放到服务器的部署目录里，确认 `deploy/portal.env` 已填写。网络名要和主站 Compose 网络一致，可用下面的命令查看：

```bash
docker network ls
```

把查到的网络名写进 `deploy/.env` 的 `PORTAL_DOCKER_NETWORK`。启动时只创建门户容器，不重建主站、数据库和 Redis：

```bash
docker compose -f deploy/docker-compose.yml up -d --force-recreate
```

第一次启动会按 `DB_SCHEMA` 创建模式和活动表。

在主站 Nginx 的站点配置里、`location /` 之前加入 `deploy/nginx-location.conf` 的内容，然后只重载 Nginx：

```bash
docker exec 你的nginx容器名 nginx -t
docker compose -f docker-compose.nginx.yml up -d --no-deps --force-recreate nginx
```

不要执行 `docker compose down -v`，那会删掉数据库卷。

## 接到主站菜单

主站管理后台里：

1. 首页内容填门户首页地址，例如 `https://你的域名/portal/`。以 `http://` 或 `https://` 开头时，主站会用嵌入页打开它。
2. 自定义菜单增加「使用教程」，地址同上。
3. 再增加「活动中心」，地址为 `https://你的域名/portal/activities`。

路径要和 `BASE_PATH`、Nginx 的 location 一致。嵌入页和主站同域，所以用户在主站登录后，门户能读到同一个登录状态。

## 领奖入账

用户点击领取后，门户先用当前登录身份向主站确认是谁，再确认这个人是本场中奖者且还没领过。然后门户使用配置里的管理员账号，调用主站 `POST /api/v1/admin/users/{用户ID}/balance`，操作类型为增加余额。

每次领取使用固定的幂等键 `CLAIM_KEY_PREFIX-活动ID-用户ID`。同一次领取如果因为网络重试再次请求，主站不会重复加钱。主站入账成功后，门户才在自己的领取表记下这笔领取。余额不直接改数据库，这样主站自己的缓存和并发扣费不会和门户打架。

## 更新主站

门户是单独的容器。更新 Sub2API 时按官方方式更新主程序即可，不要覆盖服务器上的 `portal.env`，也不要重建门户容器，除非你正在更新门户本身。
