# «Малина» 用户手册

«Малина» — 用于监控和控制能源设备的装置：MAP、KES、带 BMS 的蓄电池组以及并网逆变器。管理通过网页管理后台完成，可在手机、平板或计算机上通过网络访问。

文档按菜单页面组织。如果不知道从何开始，请打开「从何开始」一节。

![主页：已连接设备的卡片](img/main/home.png)

## 从何开始

- [从何开始](start.md) — 首次开机、连接、登录和首批必做设置。
- [管理员指南](admin.md) — SSH、访问恢复、服务、日志、网络模式。

## 设备

- [MAP](pages/map.md) — 当前状态、铭牌和 MAP 控制。
- [MAP — 设置](pages/map-settings.md) — 启用轮询和日志级别。
- [KES](pages/kes.md) — 面板发电量、蓄电池充电、继电器。
- [KES — 设置](pages/kes-settings.md) — 启用轮询和日志级别。
- [BMS](pages/bms.md) — 电池状态、电芯、温度、报警。
- [BMS — 设置](pages/bms-settings.md) — 网关、BMS 接收器和 BMS 代理。
- [并网逆变器](pages/grid.md) — Deye 和 Sofar：发电量、故障、图表。
- [并网逆变器 — 设置](pages/grid-settings.md) — 逆变器列表、位置、轮询周期。

## 网络与数据

- [网络设置](pages/network.md) — 接入点或接入外部 Wi-Fi、地址、DHCP、为客户机提供上网。
- [统计：图表](pages/stats-charts.md) — 参数变化趋势、对比、实时模式。
- [统计：表格](pages/stats-tables.md) — 精确数值与时间、历史数据分页查看。

## 系统

- [系统信息](pages/info.md) — 设备健康状况、负载、磁盘、网络、已连接设备与进程。
- [系统设置](pages/system.md) — 名称、时区、日志、MAP 时间、统计、监控、密码、电源。
- [服务](pages/services.md) — 服务状态、启动与停止、重启。
- [日志](pages/logs.md) — 查看系统日志。
- [电子邮件](pages/email.md) — 发送邮件通知。
- [事件管理器](pages/events.md) — 「条件 → 动作」规则。

## 其他

- [主节点](pages/master.md) — 多台「Малина」设备的概览。
- [主节点 — 设置](pages/master-settings.md) — 节点列表。
- [登录](pages/login.md) — 登录网页管理后台并选择语言。

## 开发者 API

- [Native API：lists, raws](api-native.md) — 令牌、设备列表、原始数据、写入与统计。
- [Legacy API](api-legacy.md) — 全部旧 API：`read_json.php`、`read_memory.php`、`check_daemons.php`、`read_bms.php`、`read_sec.php`，通过 `write_*` 写入。