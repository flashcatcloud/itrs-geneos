# Geneos → FlashDuty

[English](README.md) | 简体中文

`geneos-flashduty` 是一个独立的 Go 可执行程序，将 ITRS Geneos Alerting Effect 或 Rule Action 转换为 FlashDuty 标准告警事件。

## 工作方式

```text
Geneos Alerting + Effect --\
                            +--> geneos-flashduty --> FlashDuty Standard Alert API
Geneos Rule + Action -------/
```

程序每次事件执行一次，不启动常驻服务，也不保存本地状态。触发和恢复通过确定性的 `alert_key` 关联：

```text
_VARIABLEPATH 存在
  geneos:v1:<SHA-256(_VARIABLEPATH)>

_VARIABLEPATH 缺失、稳定组件充分
  geneos:v1:fallback:<SHA-256(canonical-components)>

稳定组件也不足
  geneos:v1:random:<UUID>
```

随机回退可以保证事件仍被发送，但无法保证后续恢复与原告警关联。完整 `_VARIABLEPATH` 在存在时还会作为 `geneos_variable_path` 标签发送，便于检索和排查；即使标签因长度限制被截断，`alert_key` 仍使用完整路径计算。

## 状态映射

| Geneos 上下文 | FlashDuty `event_status` |
| --- | --- |
| `_ALERT_TYPE=clear` | `Ok` |
| `_SEVERITY=OK` | `Ok` |
| `_SEVERITY=CRITICAL` | `Critical` |
| `_SEVERITY=WARNING` | `Warning` |
| `_SEVERITY=INFO/UNDEFINED` | `Info` |

`resolve` 子命令始终发送 `Ok`。FlashDuty 标准事件没有 PagerDuty `acknowledge` 的等价状态，因此 Geneos suspend/assign 不映射为认领；认领、值班和升级由 FlashDuty 管理。

## 安装与构建

可以从 [GitHub Releases](https://github.com/flashcatcloud/itrs-geneos/releases) 下载 Gateway 主机对应的二进制，并使用同一 Release 中的 `SHA256SUMS` 校验文件完整性。例如：

```bash
install -m 0755 geneos-flashduty-v1.0.0-linux-amd64 \
  /opt/itrs/gateway/gateway_shared/geneos-flashduty
```

要求 Go 1.22 或更新版本。

```bash
go test ./...
go build -o geneos-flashduty ./cmd/geneos-flashduty
```

构建 Linux AMD64：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o geneos-flashduty-linux-amd64 ./cmd/geneos-flashduty
```

## 安装

1. 将二进制复制到 Gateway 主机，例如：

   ```text
   /opt/itrs/gateway/gateway_shared/geneos-flashduty
   ```

2. 将 `flashduty.example.yaml` 复制为以下任一位置：

   ```text
   ./flashduty.yaml
   $HOME/.config/geneos/flashduty.yaml
   /etc/geneos/flashduty.yaml
   ```

   也可以通过 `--config PATH` 显式指定。文件优先级为显式路径、当前目录、用户配置目录、系统配置目录。

3. 在 FlashDuty 创建“自定义告警事件”集成并取得 `integration_key`。

4. 推荐在 Geneos Managed Entity 或 Managed Entity Group 上增加属性：

   ```text
   FLASHDUTY_INTEGRATION_KEY=<your-integration-key>
   ```

   Geneos 会把属性传递给 Action/Effect。该环境变量优先于 YAML 中的 `integration_key`，因此不同 Managed Entity 可以路由至不同 FlashDuty 集成。

   **触发和恢复必须使用同一个 FlashDuty integration key。** 如果两次事件被路由到不同集成，即使 `alert_key` 相同也无法关联。

5. 导入或参考：

   - `examples/geneos-effect.xml`：推荐的 Alerting Effect 配置；
   - `examples/geneos-action.xml`：Rule Action 配置。

使用 Rule Action 时，必须确保规则在恢复为 `OK` 时也执行该 Action。使用 Alerting Effect 时，Geneos 的 `_ALERT_TYPE=clear` 会自动转换为恢复事件。

## 配置

参考 [`flashduty.example.yaml`](flashduty.example.yaml)。YAML 只需写需要覆盖的字段，其他值沿用内置默认配置。

最小配置示例：

```yaml
flashduty:
  endpoint: https://api.flashcat.cloud/event/push/alert/standard
  integration_key: your-integration-key
```

`flashduty.endpoint` 可以随 FlashDuty 推送地址变化进行修改；未配置时使用上述内置默认地址。程序会把 integration key 作为 `integration_key` 查询参数自动追加到 endpoint，即使 endpoint 已经包含其他查询参数也可以正常处理。

标题、描述和自定义标签值支持 `${NAME}` 环境变量模板。以下内容不会进行模板展开：endpoint、integration key、超时、重试次数、key 前缀和 severity map。

`FLASHDUTY_INTEGRATION_KEY` 不会输出到日志。程序也不会默认上传所有 Geneos 环境变量，只发送预定义的监控上下文和配置的自定义标签。

## 命令

自动判断 Geneos Effect/Action 事件：

```bash
geneos-flashduty
```

显式触发或恢复：

```bash
geneos-flashduty trigger --variable-path '/geneos/.../cell'
geneos-flashduty resolve --variable-path '/geneos/.../cell'
```

测试配置和连通性：

```bash
geneos-flashduty test --config /etc/geneos/flashduty.yaml
```

测试命令会先发送一条 `Info`，随后用相同 `alert_key` 发送 `Ok`，避免留下故意打开的测试告警。

## 可靠性

- 默认单次请求超时：10 秒；
- 默认重试：初始请求后最多重试 3 次；
- 网络错误、HTTP 429 和 5xx 会重试；
- 其他 4xx 不重试；
- 遵循 `Retry-After`，最大等待 60 秒；
- 成功退出码为 0，失败为非零；
- 成功日志包含 FlashDuty `request_id`；
- 日志中的 integration key 会被脱敏。

## 排查

- `FlashDuty integration key is required`：配置 YAML key 或 Geneos 属性 `FLASHDUTY_INTEGRATION_KEY`。
- HTTP 401/403：检查集成推送地址和 key 是否属于同一个集成。
- 告警无法恢复：确认触发和恢复使用相同 `_VARIABLEPATH`、相同 key 算法版本和相同 FlashDuty integration key。
- 日志出现 `alert_key_source=random`：Geneos 没有提供足够的稳定身份字段，后续恢复无法保证关联；优先检查 Action/Effect 的触发上下文。
- Gateway 无法访问：确认 Gateway 主机可以通过 HTTPS 访问 `api.flashcat.cloud`。

## 开发与发布

```bash
gofmt -w .
go test -race ./...
go vet ./...
go build ./cmd/geneos-flashduty
```

推送匹配 `v*` 的版本标签后，GitHub Actions 会构建 Linux 和 macOS 的 AMD64/ARM64 可执行文件，生成 `SHA256SUMS` 并创建 GitHub Release。

## 许可证

[MIT](LICENSE)
