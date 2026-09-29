# 网络拓扑设计平台（第一轮：节点 / 链路 / 策略工程）

离线的网络拓扑设计与可达性分析平台：**Go 负责规则计算，React 负责编辑**。
平台不连接任何真实网络，所有结论都针对某个**不可变拓扑版本**的内存快照计算；
后续容量规划复用同一版本模型（在 `Topology.Meta` 与节点/链路上扩展即可）。

## 可达性怎么判定

1. 链路是**有向**的：`from -> to` 一条边只代表一个方向，双向需要两条链路。
2. 路径按经过的**每个节点**（源、中转、目的）依次评估策略。
3. 规则按数组顺序匹配，**首条命中生效**（first-match-wins），无任何命中即 **default deny**。
4. 返回最多 K 条无环候选路径（最短优先，Yen 风格枚举），包括被阻断的绕行路径，
   并给出实际采用路径上每个节点命中的规则 ID。绝不仅因“两个节点之间有线”判定可达。
5. 改动一条策略后，可对同一组连接对比两个版本，输出**新增可达 / 被阻断**，
   并附前后原因；方向与规则顺序在两个版本中独立、完整地重算。

## 身份与版本

- 节点 / 链路 / 规则的 `id` 是稳定身份：保存、撤销、导入均原样保留，绝不重新生成。
- 每次保存产生内容寻址版本（节点 ID、链路与**有序**规则参与哈希）；撤销只切换当前版本指针。
- 编辑器本地撤销栈 + `localStorage` 自动持久化；后端另有版本栈与 `/api/undo`。

## 目录

- `backend/topo`：领域模型、规则引擎（`engine.go`）、版本仓库（`store.go`）、HTTP API（`server.go`）
- `backend/cmd/server`：仿真 API 服务（默认 `:8080`，`TOPO_ADDR` 可改）
- `frontend/`：Vite + React 19 + zustand 的拓扑编辑器与可达性 / 影响对比界面

## 运行

```bash
# 后端
cd backend && go test ./... && go run ./cmd/server

# 前端（另开终端；/api 代理到 8080）
cd frontend && npm install && npm run dev
```

## HTTP API

- `POST /api/topology` 保存当前编辑为新版本
- `POST /api/undo` 撤销到上一版本
- `POST /api/import` 导入导出 JSON（校验结构，保留 ID）
- `GET /api/topology` / `GET /api/versions` 查看当前版本与版本列表
- `POST /api/evaluate` `{conn|conns, maxPaths, version?}` 可达性与规则轨迹
- `POST /api/diff` `{fromVersion, toVersion?, conns, maxPaths}` 策略改动影响对比

## 前端操作

- 加节点后可拖动布局；选中“以此为起点连线”再点目标节点，建立**有向**链路。
- 规则面板可增删、停用、上下移动（顺序即匹配优先级），按节点 / 区域 / 协议匹配。
- “能否通过？”返回判定结论、多条候选路径及逐节点命中的规则。
- “加载示例”内置主路径 + 绕行路径的小拓扑，可直接体验策略阻断与版本对比。
