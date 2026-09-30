# 网络拓扑设计平台（第一轮：节点 / 链路 / 策略工程）

离线的网络拓扑设计工具：**Go 负责计算，React 负责编辑**。不连接任何真实网络；
每次保存生成不可变拓扑版本，后续容量规划将直接引用同一版本。

## 本轮交付的能力

- 节点、有向/无向链路的编辑（画布拖拽、连线模式、属性面板）。
- 有序策略规则：按 `order` 升序逐条评估，**首个命中的规则决定放行/拒绝**，
  均未命中时取默认动作（默认拒绝）。可匹配源/宿节点、节点类型、链路、
  链路协议与自定义属性；规则可停用。
- 一条连接“能否通过”的完整解释：返回多条候选路径（Yen K 最短路径），
  区分**可行路径**与**被阻断路径**，逐跳给出实际命中的规则 ID、规则顺序、
  放行/拒绝，以及是否反向走了无向链路。结论基于逐跳评估——
  “两点间有线”不代表可达。
- 方向不被忽略：有向链路只能沿 source→target；策略的源/宿匹配同样有向；
  差异对比遍历所有**有序节点对**。
- 策略改动对比：设定基线后修改规则，可列出新增可达与新被阻断的连接，
  并对每个改变的方向给出**逐跳归因**（`hopChanges`）：明确是哪条有序规则
  （ID + order）因新增/删除/启用/停用/重排/改动作而改变了某一段链路的
  放行/拒绝，或默认动作、链路增删、链路方向变化所致。
- 空结果也是可读结果：可达、不可达、无路由都返回稳定的 `reason` 与
  `paths: []` / `blocked: []`（序列化为 `[]` 而非 `null`），页面不会白屏。
- 同版本解释一致：K 路径搜索按节点 ID 做确定性并列裁决，保存重开同一
  不可变版本时解释完全一致；前端按编辑序号（`editSeq`）丢弃迟到响应，
  旧检查结果不会覆盖新编辑。
- 保存（版本化快照）、撤销/重做（本地编辑历史）、JSON 导入/导出，
  全程保留节点、链路、规则的既有 ID，导入不会重排身份。

## 目录结构

```
backend/
  cmd/server/          HTTP API（仅标准库）+ 集成测试
  internal/topo/       拓扑模型、有序规则评估、K 路径搜索、差异对比、校验
  internal/store/      不可变版本快照（JSON 文件，原子写入）
frontend/
  src/state/           zustand 草稿 + 撤销/重做历史
  src/components/      画布、节点/链路面板、策略面板、连通性解释、差异面板
  src/lib/             API 客户端、ID 生成、示例拓扑
```

## 运行

后端（Go 1.27，仅标准库）：

```bash
cd backend
go test ./...
go run ./cmd/server -addr :8080 -data ./data
```

前端：

```bash
cd frontend
npm install        # 本评测环境已通过共享 node_modules 预置依赖
npm run dev        # http://localhost:5173，/api 代理到 :8080
npm run typecheck
npm run test
npm run build
```

也可以用后端直接托管构建产物：

```bash
cd frontend && npm run build
cd ../backend && go run ./cmd/server -static ../frontend/dist
```

## HTTP API 摘要

- `GET /api/draft` / `PUT /api/draft`：读取/替换工作草稿。
- `POST /api/save`：把拓扑存为下一个不可变版本，返回新版本号。
- `GET /api/versions` / `GET /api/versions/{v}`：版本列表与快照。
- `POST /api/check`：body 含 `{topology?,source,target,maxPaths}`，返回
  `reachable`、`paths[]`、`blocked[]`，每条路径逐跳带规则轨迹。
- `POST /api/diff`：body 含 `{after, before? | beforeVersion, detailed}`，
  返回 `newReachable[]` 与 `newBlocked[]`（有序节点对）。
- `POST /api/import`：导入外部拓扑 JSON，只做规范化与校验，不改任何 ID。

## 语义约定（供后续容量规划复用）

- 版本快照不可变：容量规划引用 `version` 即可获得完全一致的拓扑与策略。
- 链路 `kind`：`directed`（仅 source→target）或 `undirected`（双向，
  反向经过时 hop 上标记 `reverse:true`）。
- 默认动作建议保持 `deny`：没有显式放行的跳一律不通。
- 规则顺序即优先级：调整顺序会改变语义（测试中有正反例）。
