<div align="center">

<img src="docs/assets/logo.jpg" alt="iso-topology logo——文本 DSL 变成等距风格的方块架构图" width="440">

# iso-topology — 用代码画等距架构图

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Go Reference](https://pkg.go.dev/badge/github.com/MarkovWangRR/iso-topology.svg)](https://pkg.go.dev/github.com/MarkovWangRR/iso-topology)

### Figma 质感的等距(2.5D)架构图——以代码为源,为 agent 而生。

**用自然语言向你的 agent 提需求,用同样的方式改图,最后把结果提交进仓库。**

![ClickHouse 实时分析——iso-topology 渲染的暗色毛玻璃等距架构图](docs/assets/clickhouse-hub.png)

<sub>↑ 这张图就是"问"出来的——*「画一个 ClickHouse 实时分析架构,暗色玻璃风格」*——再加几句口头微调(*「连线加粗、虚线、带渐变」「背景加斜向网格」「点缀色用青柠」*)。没有 Figma,没有坐标,只有一段可以 diff 的文本。</sub>

`单个静态二进制` · `零运行时依赖` · `毫秒级渲染` · `200+ 图标(含真品牌 logo)` · `所有样例都有 golden 测试`

[English](README.md) · 简体中文

</div>

---

这是唯一一个输出达到设计稿水准的**等距**视觉、DSL 又是一份机器可校验的契约(agent 能对着它自我纠错)、还能**离线在单个静态二进制里确定性运行**的图表即代码工具。替代方案要么是在 Figma 里耗一下午(没法 diff,架构一变图就废),要么是 Mermaid/D2(平面流程图)。iso-topology 让你从文本得到 Figma 的观感——agent 生成、渲染前校验、和代码放在一起提交。

## 想自己手写?它只是文本。

preset 把外观定义一次;`layout` / `place` 关系负责定位——从头到尾没有像素坐标。从这段:

```yaml
theme:
  presets:
    heroBlue:                                  # 全图唯一的发光强调色,按名字复用
      stroke: { color: "#6FA8E0", width: 1.6 }
      faces: { top: { fill: { kind: linearGradient, stops: [ {offset: 0, color: "#1A3550"}, {offset: 1, color: "#0E1C30"} ] } } }
      effects: { faceSplit: true, backglow: { color: "#6BA8E8", radius: 70 } }
nodes:
  scene:
    parts:
      - id: runtime
        shape: group
        layout: { mode: column }               # ← 自动排布,不写坐标
        parts:
          - { id: agent,  preset: heroBlue, icon: "iso://si/langchain/8FBEEF", label: "LangGraph Agent" }
          - { id: memory, icon: "iso://glyph/database", label: "Memory · State" }
    connectors:
      - { from: client, to: runtime, routing: orthogonal,
          stroke: { gradient: { from: "#3B6FD4", to: "#8FBEEF" } } }   # 发光的数据流
```

……渲染出这张图——一个典型的 LangChain agent 应用,用的正是 LangChain 自家的暗色冷蓝风格:

![LangChain agent 应用架构——深海军蓝舞台,发光的冷蓝 LangGraph 运行时连接客户端、模型、检索、工具,配 LangSmith 可观测性](docs/assets/langchain-app.png)

确定性求解器把关系变成几何——这里的每一张图都是零坐标的。
[源码](samples/topology/langchain-app/input.yaml)。

## 60 秒上手

**自己动手**——单个静态二进制,不需要浏览器和字体:

```bash
go install github.com/MarkovWangRR/iso-topology/cmd/isotopo@latest
echo 'user -> api -> db' > scene.d2     # 任意 d2 图 → 自动布局
isotopo render scene.d2 ./out           # → out/topology.svg(+ html + 每节点独立 SVG)
isotopo serve  scene.d2                 # → localhost:8731——拖拽、改样式、导出
```

**交给你的 agent**(Claude Code、Cursor 等)——接一次就好(以下命令均经过验证):

```bash
go install github.com/MarkovWangRR/iso-topology/cmd/isotopo@latest      # 渲染器
go install github.com/MarkovWangRR/iso-topology/cmd/isotopo-mcp@latest  # MCP 服务
claude mcp add isotopo -- isotopo-mcp                                   # 注册进 Claude Code
```

之后用自然语言提需求、用自然语言改图——`capabilities → validate → render` 这套循环由 agent 替你跑:

> *「画一个 ClickHouse 实时分析架构,暗色玻璃风格」*
> *「连线加粗,改虚线,带渐变」*
> *「背景加斜向网格,节点描边提亮一点」*

整个工作流就是:**提需求 → 微调**。比起 MCP 更想用 Claude Code 技能?`git clone` 本仓库后运行
`scripts/install-skill.sh`。完整的系统提示词在
[PROMPT_TEMPLATE.md](docs/agent/PROMPT_TEMPLATE.md);其他运行时参考 [`llms.txt`](llms.txt)。

## agent 跑的那套循环

iso-topology 讲契约,不讲手感——agent 先探明 DSL,写出场景,收到机器可校验的反馈,然后在没有人介入的情况下收敛:

```bash
isotopo capabilities          # 机器可读的 DSL 能力清单——每会话读一次
isotopo validate scene.yaml   # JSONPath 定位的问题 + “你是不是想写……”修复建议(exit 0/2/3)
isotopo evaluate scene.yaml   # 评分卡:交叉 / 重叠 / 穿越 + 构图得分
isotopo repair   scene.yaml --compose --write   # 把自动修复写回源文件:遮挡、重叠、
                                                # 对比度 + 有界的对齐吸附
isotopo render   scene.yaml out
isotopo snapshot scene.yaml out                 # + 忠实的 topology.png,供视觉自检
isotopo preview  scene.yaml out.svg core edge:3   # 只裁剪某个节点 / 组 / 边
```

`validate` 是带修复建议的 JSON-schema 检查;`evaluate` 在确定性的平面视图上同时给
缺陷分和构图分(均衡 / 对齐 / 间距节奏 / 主角权重 / 强调色纪律);`repair` 对两者都能
下手——修掉缺陷,并把偏离轨道的部件吸附到邻居的对齐轨道上,再把修复结果连同注释一起
写回源文件。这一切让 agent 在看到一个像素之前就能自我纠错。输出是字节级确定的——同样
的输入,同样的 SVG——golden 测试和干净的 Git diff 都建立在这上面。

## MCP 服务

`isotopo-mcp` 把整套循环以七个 stdio 工具的形式暴露给任何 MCP 客户端(Claude Code、
Claude Desktop、Cursor 等)——`iso_capabilities`、`iso_validate`、`iso_evaluate`、
`iso_repair`、`iso_render`、`iso_snapshot`、`iso_preview`——agent 画图、自我纠错、
再对自己的产出做视觉检查(validate → repair → render → snapshot),全程不用调 shell:

```bash
go install github.com/MarkovWangRR/iso-topology/cmd/isotopo-mcp@latest
claude mcp add isotopo -- isotopo-mcp        # Claude Code
```

Claude Desktop / Cursor / 任何通用客户端——在它的 MCP 配置里加上:

```json
{
  "mcpServers": {
    "isotopo": { "command": "isotopo-mcp" }
  }
}
```

完整工具参考 + MCP 版的循环:[docs/agent/MCP.md](docs/agent/MCP.md)。

## Studio——点选、拖拽、改样式

```bash
isotopo serve input.yaml        # → http://localhost:8731
```

![isotopo Studio——左边是渲染好的等距场景,右边是可编辑的 YAML](docs/assets/studio.png)

左边是渲染结果,右边是它的 YAML,中间是亚秒级的反馈。**拖拽排版**、把节点拖进/拖出分组、
**右键改样式**、增删/复制/连接节点、◳ 等距 / ◰ 平面视图切换、撤销/重做,以及导出
SVG / PNG / YAML。所有编辑都发生在浏览器内的副本上——不点保存,磁盘上的文件绝不会被动。
导览:[docs/guides/studio.md](docs/guides/studio.md)。

## 你能画什么

- **18 种等距图元**——方块、圆柱、棱柱(三/六/八棱)、机架、阵列(1D/2D/3D)、云、
  人形、边界(VPC/区域)、楔形、自定义多边形——按*角色*选形状,而不是清一色的矩形。
- **零坐标布局**——`place: {rightOf|behind|above…, gap}` 关系 +
  `layout: {mode: row|column|grid|ring|auto}` 容器(auto = 按连线求解 DAG)。
  求解器负责计算位置、校验引用、并对重叠发出警告。
- **设计稿级样式**——逐面的纯色/渐变填充、半透明**玻璃**、**颗粒**质感、`faceSplit`
  受光、点阵/斜纹纹理(按等距投影)、投影、辉光、圆角、线框。
- **读起来像系统的连线**——`orthogonal`(沿等距网格走线)/ `straight` / `bezier`,
  支持虚线、渐变、途经点和标签。
- **一份源码,两种投影**——默认是设计稿级的**等距**视图;想要极致易读的时候切换成
  平面的 **2D 俯视**“文档”视图。渲染时 `--projection top`,或在场景里写
  `canvas: { projection: top }`——同一份 DSL,同一个布局求解器,不用重写。
  参见 [`samples/topology/plan-view-2d`](samples/topology/plan-view-2d/input.yaml)。
- **命名主题 + 语义角色**——只写纯拓扑(`role: hero | tray | chip`),再挑一个外观:
  `--theme clickhouse-dark | navy-glass | handdrawn-paper | clean-light`
  (或在 DSL 里写 `theme: { use: <name> }`)。主题提供整套设计系统——角色样式、
  尺寸节奏、文字、画布,甚至会为深色填充自动适配图标墨色——同一个场景可以整体换肤,
  一个普通的 `.d2` 图也能靠一个 flag 得到完整的外观。
- **一个 flag 换来可读性**——`isotopo render --readable` 给任意场景叠加一层以易读为先的
  “文档”配置:直立的屏幕空间标签、感知画布的对比度底衬、内边距下限。可选且无损——
  它只填补你留空的部分,精心调过的场景保持原样。
- **200+ 可改色图标**——150+ 真品牌 logo(Simple Icons,CC0)加 35 个概念图形;
  任何图标都能用 `/RRGGBB` 或 `/light` 改色。
- **可复用的外观**——`theme.presets`,外加 28 份即拿即用的
  [风格参考](samples/style_refer):按气质挑一种视觉语言,复制它的
  `theme.presets` + `canvas`,再把你的拓扑叠上去。

机器可读的完整清单:`isotopo capabilities`(或
[CAPABILITIES.md](docs/agent/CAPABILITIES.md))。

## 画廊

**ClickHouse 生态**——暗色毛玻璃的轮辐结构,唯一的霓虹强调色。[源码](samples/topology/clickhouse-hub/input.yaml)

![ClickHouse 生态等距图](docs/assets/clickhouse-hub.png)

**DuckDB 数据架构**——手绘风:暖色纸面、等距网格、胶片颗粒。[源码](samples/topology/duckdb-handdrawn/input.yaml)

![DuckDB 手绘风等距图](docs/assets/duckdb-handdrawn.png)

**Databricks 湖仓 + LLM 智能体**——忠于品牌,暖色画布上唯一的 Lava 强调。[源码](samples/topology/lakehouse-agent/input.yaml)

![Databricks 湖仓与 LLM 智能体等距图](docs/assets/lakehouse-agent.png)

**训练算力**——亮色画布,虚线的预算“幽灵”体积。[源码](samples/topology/training-compute/input.yaml)

![等距的训练算力柱状图](docs/assets/training-compute.png)

## 为什么是等距,为什么是这个工具

平面 2D 读起来是一排方框;等距图读起来是一个**系统**——纵深方向一眼分出接入层 / 中间层 /
数据层,叠起来的节点天然表达副本和高可用。而在 Figma 里手绘等距图,规模到十来个元素
就到头了,还没有一个人能 review 它的 diff。

| | Mermaid / D2 | Figma / draw.io | **iso-topology** |
|---|---|---|---|
| 视觉档次 | 平面流程图 | 设计稿水准 | **设计稿水准的等距图** |
| 文本源码、可 git diff | ✓ | ✗ | ✓ |
| agent 可探明并校验 DSL | ✗ | ✗ | **✓(`capabilities` + `validate`)** |
| 不需要手调坐标的布局 | ✓ | ✗ | ✓(`place` / `layout` 求解器) |
| 离线单文件运行 | 部分 | ✗ | ✓ |

## 当 Go 库用

```go
import isotopo "github.com/MarkovWangRR/iso-topology"

svg, issues, _ := isotopo.RenderSource("yaml", yamlBytes)            // DSL → 一张 SVG

op := isotopo.EditOp{Kind: "move", Target: "node", ID: "api", DWX: 50}
newSrc, svg, issues, _ := isotopo.ApplyOp("yaml", yamlBytes, op)     // 无状态画布编辑,注释保留
```

这套无状态编辑契约支撑着 Studio 和浏览器端 WASM 编辑器。完整接口:
[API 参考](docs/reference/api.md)([godoc](https://pkg.go.dev/github.com/MarkovWangRR/iso-topology))。

## 文档

总索引在 [docs/README.md](docs/README.md)。

- **入门:**[教程](docs/getting-started/01-install.md) · [常用写法](docs/agent/RECIPES.md) · [画面设计](docs/guides/scene-design.md)
- **参考:**[API](docs/reference/api.md) · [CLI](docs/reference/cli.md) · [YAML DSL](docs/reference/dsl-yaml.md) · [d2 DSL](docs/reference/dsl-d2.md) · [样式/主题](docs/reference/dsl-theme.md)
- **Agent:**[CAPABILITIES](docs/agent/CAPABILITIES.md) · [PROMPT_TEMPLATE](docs/agent/PROMPT_TEMPLATE.md) · [SAMPLES](docs/agent/SAMPLES.md) · [schema](docs/agent/schema/dsl.schema.json) · [MCP](docs/agent/MCP.md) · [skills](skills/README.md)

## 项目状态与参与

个人项目,迭代很快——如有依赖请锁定 tag(`d2` 依赖已固定在 `v0.7.1`)。欢迎 issue 和
PR;提交前请跑 `go test ./...`——`samples/*/*/expected.svg` 是 golden 文件,任何输出
漂移都会被拦住。画出了得意之作?开个 issue 贴上你的场景和 SVG,优秀作品会被收录到这里。
点个 ⭐ 能让更多人找到它。

## 许可证

Apache License 2.0——见 [LICENSE](LICENSE)。渲染产物归你所有,可以商用;内置的品牌
logo 来自 [Simple Icons](https://simpleicons.org),CC0 协议。
