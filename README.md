# 网络拓扑设计平台（第二轮：连接状态流量检查）

离线的网络拓扑设计工具：**Go 负责计算，React 负责编辑**。不连接任何真实网络；
每次保存生成不可变拓扑版本，后续容量规划将直接引用同一版本。

## 本轮交付的能力

- 经过地址转换设备的连接检查：节点可配置 NAT（转换后地址 + 端口池），
  同一条连接在路径不同位置呈现不同的源地址/端口。`POST /api/conncheck`
  返回完整的**地址转换轨迹**（`natEvents`）：每次匹配采用的地址、所在
  设备、转换决定（新建映射 / 命中映射 / 映射超时 / 端口被占用 /
  无对应映射 / 转换池耗尽 / 范围已改变）及依据，去程、回程与迟到回程
  分别标注。
- 回程必须找到对应映射：回程包按完整四元组（转换后地址端口 + 对端
  地址端口）匹配映射，**一个转换后的端口不会被认为是所有旧连接都可
  复用**；对端不一致即无法确认，直接丢弃。
- 迟到返回包的归属说明：连接空闲超时、转换范围改变（`natRanges`
  试算）或端口被新连接重新使用后，迟到回程包说明为何属于旧连接或
  为何无法确认（`late-return` 事件），**绝不放行到后来建立的另一条
  连接**。
- 已有连接沿用原转换，新连接重新选择：`keptMappings`（已有连接未
  改动的映射）与 `newMappings`（本次新连接按最低可用端口确定性分配）
  分别展示；两台主机争用同一映射池时先登记者保持、后来者取下一个
  可用端口，池耗尽则新连接被拒绝（`nat-pool-exhausted`）。
- 非对称返回经过不同设备不算可达：回程路径经过未参与去程转换的
  NAT 设备时无映射可命中（`no-mapping`），明确标注不能凭去程已放行
  认定回程路径有效。
- 方案对比覆盖转换状态：`POST /api/conndiff` 的已有连接影响新增
  `mappingStatus`（kept / invalidated-range / device-removed）——
  转换范围调整或设备不再承担 NAT 时，即使路径仍放行，既有连接也判为
  `broken-nat`；两段连续转换、两台主机争用映射、规则重排与中间设备
  断开可组合试算，全部为纯计算，不修改草稿与任何已保存版本，未确认
  的连接关系不影响其他方案。

## 第二轮能力（保留）

- 首次加载不再白屏：版本列表、差异报告等空集合在后端一律序列化为
  `[]`（曾为 `null` 导致前端 map 崩溃整页空白），前端再做一层防御性
  归一化；刷新可正常进入编辑。加载不到的资料在原位置说明——草稿恢复
  失败时页面顶部明示原因，并标注当前显示的是示例占位拓扑而非用户保存
  的内容；空数据目录的示例也明确标注“未保存草稿”，绝不冒充已保存方案。
- 有连接状态的流量检查：规则可限定连接状态（`connState`：新连接 /
  已建立 / 回程，空为任意阶段）。首次请求（state=new）、已建立连接的
  保持流量（state=established）与回程流量（state=return，方向反转）
  可遵守不同规则；连接检查按“建立 / 保持 / 回程 / 失效”四个阶段沿路径
  逐跳给出依据。
- 非对称返回不算全程可达：去程放行但回程被阻断时明确标注“非对称返回”，
  `bidirectional=false`，绝不凭借一次放行认作全程可达。
- 修改策略或路由后分别说明影响：`POST /api/conndiff` 对新连接
  （state=new，全有序节点对）与已登记的已有连接（state=established，
  按原路径逐段评估）分别给出影响——已有连接可能保持（survives）、
  因路径断开（broken-path）或因策略拒绝（broken-policy），并给出具体
  段与规则归因。
- 试算不改变其他方案：连接检查支持模拟连接空闲超时、断开中间节点 /
  链路，全部为纯计算，不修改草稿与任何已保存版本；不同流量阶段的试算
  结果按版本缓存（localStorage），重开后每段解释仍对应原拓扑、策略及
  连接状态，可比较规则重排、连接超时与中间节点断开的组合。

## 第一轮能力（保留）

- 刷新 / 重启不丢工作：工作草稿随每次编辑、保存、导入持久化到
  `draft.json`，重新打开时恢复用户保存的拓扑、规则顺序与默认动作；
  按版本缓存的连通性解释（localStorage）随拓扑一并恢复。空数据目录
  才显示示例，且明确标注为“未保存草稿”，不会冒充已保存版本。
- 多策略方案切换与比较：已保存版本即方案，可在方案间切换并两两对比；
  逐跳归因（`hopChanges`）标明是哪条有序规则（ID + order）、默认动作
  或链路变化使某段路径在直达 / 绕行 / 不可达之间切换。
- 未保存草稿受保护：有未保存修改时切换方案会显式弹出选择——
  保存为新版本并切换 / 丢弃修改并切换 / 取消；保存永远生成新版本，
  历史方案不可变、不会被覆盖。
- 解释始终对应当前版本：检查结果带版本标签，编辑与方案切换都会使
  在途响应失效（`editSeq`  staleness 丢弃），撤销/重做同样递增序号。
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
- `POST /api/conncheck`：body 含 `{topology?,source,target,maxPaths,
  downNodes?,downLinks?,timeout?,srcPort?,dstPort?,existing?,
  expiredConns?,natRanges?}`，返回连接生命周期四阶段（建立 / 保持 /
  回程 / 失效）的逐跳解释、`state`、`asymmetric`、`bidirectional`
  结论，以及地址转换轨迹 `natEvents`、新连接映射 `newMappings` 与
  已有连接保留映射 `keptMappings`。
- `POST /api/conndiff`：body 含 `{after, before? | beforeVersion,
  connections?, detailed}`，返回 `newConns`（新连接有序对差异）与
  `existing[]`（每条已有连接的保持/断开影响）。
- `POST /api/diff`：body 含 `{after, before? | beforeVersion, detailed}`，
  返回 `newReachable[]` 与 `newBlocked[]`（有序节点对）。
- `POST /api/import`：导入外部拓扑 JSON，只做规范化与校验，不改任何 ID。

## 语义约定（供后续容量规划复用）

- 版本快照不可变：容量规划引用 `version` 即可获得完全一致的拓扑与策略。
- 节点 `nat` 块：`{enabled,address,portStart,portEnd}`，启用后该节点即
  地址转换设备；主机地址取节点 `attrs.ip`（缺省回退为节点 ID）。
- NAT 映射按完整四元组匹配：转换后地址端口 + 对端地址端口共同确认
  回程包归属，端口复用只在旧映射失效后发生，且迟到包不会交给新连接。
- 链路 `kind`：`directed`（仅 source→target）或 `undirected`（双向，
  反向经过时 hop 上标记 `reverse:true`）。
- 默认动作建议保持 `deny`：没有显式放行的跳一律不通。
- 规则顺序即优先级：调整顺序会改变语义（测试中有正反例）。
