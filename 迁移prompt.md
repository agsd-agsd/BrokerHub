# BrokerHub 费率管理迁移 Prompt

## 1. 迁移目标

你当前的任务不是把整个旧仓库原样复制到新的 fork，而是把 **BrokerHub 中与 LiquidityPool 费率管理迁移相关的成果** 做一次干净迁移。  
目标是：在新的 fork 仓库里恢复一套结构清晰、接口稳定、实验可复现、绘图可继续使用的实现。

这次迁移的主题是：

- 将 LiquidityPool 论文中的费率管理思想整理并落到 BrokerHub
- 保留已经稳定的公共接口与绘图入口
- 去掉当前仓库中明显为“调图”“试错”“临时塑形”而加的硬编码启发式
- 把尚未完成的实验任务继续保留为明确工作包

这次迁移是 **clean-port**，不是 raw copy。  
你需要明确区分：

- 哪些内容直接迁移
- 哪些内容只迁移接口
- 哪些内容必须按行为重写
- 哪些内容只作为参考文档，不进入最终主线

## 2. 信息源优先级

迁移时严格按下面的优先级理解需求：

### 第一优先级：论文

论文文件：`C:/Users/ASUS/Desktop/origin/LiquidityPool__camera-ready.pdf`

论文负责定义“机制想表达什么”。  
尤其是以下结论必须作为迁移时的机制目标：

- 非 monopoly 阶段，Hub 会通过降低 MER 吸引更多 broker 和 AUM
- 在合适条件下，AUM 的增长可能抵消 MER 的下降，从而带来更高 revenue
- 一旦某个 Hub 形成 dominance / monopoly，它会尝试提高 MER 来最大化利润
- 当提高 MER 超过某个阈值时，会触发 broker 的骤然流失
- 这个阈值需要被记住，并作为之后 fee 上限的参考
- 论文真正想表达的不是“双 Hub 永久稳定共存”，而是：竞争、dominance、shock、memory 之后更容易走向 monopoly 或稳定偏置格局

### 第二优先级：`todolist_403.md`

文件：`C:\Users\ASUS\Desktop\origin\BrokerHub\todolist_403.md`

这个文件负责定义“你要在 BrokerHub 里观察到什么实验现象”。  
它不是论文替代品，但它定义了本项目当前真正要交付的行为目标，包括：

- Competition / Dominance / Shock / Memory 四阶段的费率机制
- broker utility 决策逻辑
- `CriticalMERCap` 的记录与长期约束
- `Initial Allocation Asymmetry`
- alpha 连续扫描实验
- alpha 结果绘图
- 最终实验验收标准

### 第三优先级：当前 BrokerHub 中已稳定的接口与脚本

当前仓库中已经稳定、适合直接带走的是：

- optimizer 抽象层
- `taxrate` 方案与其测试
- committee 到 optimizer 的数据输入方式
- CSV 的基础结构和扩展调试列
- 绘图脚本三件套
- CLI 参数模式和模式切换方式

这些内容优先按接口原样保留，不要无谓重构。

### 第四优先级：当前 `paper_monopoly` 实现

当前仓库中的 `paper_monopoly` 是一系列探索性原型，不是最终规范。  
它可以提供：

- 你曾经尝试过什么方向
- 目前有哪些 phase / debug 字段 / utility 入口
- 哪些点已经接线成功

但不能作为逐行复制依据。  
尤其不能把当前为了“让图像更像预期”而加入的临时 phase bias、flow multiplier、lock-in 偏置、调参常数直接当作最终实现。

## 3. 当前应保留的公共接口与运行方式

下面这些公共接口和运行方式应尽量在 fork 中保持一致：

### 3.1 Fee Optimizer 抽象

保留统一接口：

```go
type FeeOptimizer interface {
    Optimize(input EpochMetrics) float64
    FeeRate() float64
    MinFee() float64
    DebugState() FeeOptimizerDebug
}
```

要求：

- `committee` 层只能依赖这个接口，不应直接绑死某个具体 optimizer struct
- `taxrate` 和 `paper_monopoly` 都必须走统一接口

### 3.2 Optimizer 输入结构

继续保留 `EpochMetrics` 作为 optimizer 的统一输入。  
它至少应包含：

- `Iteration`
- `ParticipationRate`
- `CurrentFunds`
- `CurrentEarn`
- `StrongestCompetitorFunds`
- `StrongestCompetitorEarn`
- `Transactions`
- `BrokerCount`

### 3.3 CLI 运行模式

下面这些 CLI 模式继续保留：

- `--fee_optimizer taxrate`
- `--fee_optimizer paper_monopoly`
- `--sim_seed`
- `--exchange_mode`

现有语义保持不变：

- `taxrate`：默认模式，接近 LiquidityPool Python `tax_optimizer.py`
- `paper_monopoly`：论文风格 monopoly fee control 模式
- `sim_seed=0`：非固定随机
- `sim_seed>0`：可复现实验

### 3.4 绘图入口

继续保留：

- `python draw_diff_hub.py`
- `python draw_diff_hub_extended.py`

不要改默认用法，不要要求必须传新参数。

## 4. 论文中的费率管理方法提炼

迁移实现时，必须把论文里的费率机制提炼成行为目标，而不是停留在术语。

### 4.1 非 monopoly 阶段

机制目标：

- 当 Hub 还没有形成明显 dominance 时，降低 MER 应该优先起到“抢 broker / 抢 AUM”的作用
- revenue 不能只按“MER 越高 revenue 越高”理解
- 在竞争阶段，`MER ↓` 后，可能出现：
  - `fund ↑`
  - `participation ↑`
  - 在 1 到 3 个 epoch 内 `revenue ↑`

因此系统应允许出现这类片段：

`MER 下降 -> broker 增加 -> fund 增加 -> revenue 在滞后几轮后提升`

### 4.2 dominance / monopoly 阶段

机制目标：

- 当一个 Hub 明显领先后，它应该开始尝试小步提 MER
- 这个提费不应该一口气冲顶
- 提费后应该观察 broker / fund / participation 的反馈

### 4.3 shock 阶段

机制目标：

- 如果领先 Hub 提费后出现明显 broker 流失、participation 下跌或资金下跌，就认为发生 shock
- shock 触发时，要记录触发前的管理费率临界值 `CriticalMERCap`
- 之后当前 fee 要迅速回落到 `CriticalMERCap` 下方

### 4.4 memory 阶段

机制目标：

- 一旦某个 Hub 记录过 `CriticalMERCap`，后续不允许长期高于这个阈值
- 但也不能变成一条死平线
- 正确形态应该是：在 `CriticalMERCap` 下方一个窄区间内缓慢活动/试探

### 4.5 论文真正想要的叙事

迁移后的系统应该服务于下面这条叙事：

1. 先竞争
2. 再出现优势方
3. 优势方开始提费
4. 提费过头会触发流失
5. 系统记住这个 fee 阈值
6. 后续领先 Hub 在阈值下方活动并逐渐锁定 monopoly

## 5. BrokerHub 当前稳定可迁移部分

以下内容可以近似直接迁移，不建议重写：

### 5.1 Optimizer 抽象层

直接迁移：

- `supervisor/optimizer/fee_optimizer.go`

要求：

- 保留 `FeeOptimizer`
- 保留 `FeeOptimizerDebug`
- 保留当前 `committee` 与 optimizer 的解耦方式

### 5.2 `taxrate` 模式

直接迁移：

- `supervisor/optimizer/tax_rate_optimizer.go`
- `supervisor/optimizer/tax_rate_optimizer_test.go`

要求：

- 保持默认模式不变
- 保持和 `EpochMetrics` 的对接方式不变
- 保持现有测试通过

### 5.3 参数模式

直接迁移：

- `params/fee_optimizer_mode.go`
- `params/fee_optimizer_mode_test.go`
- `params/exchange_mode.go`
- `params/exchange_mode_test.go`

要求：

- `taxrate` / `paper_monopoly` 的模式切换语义保持一致
- `limit100` / `limit300` / `infinite` 的 epoch 语义保持一致

### 5.4 Committee 到 Optimizer 的数据接线

从当前 `committee_brokerhub.go` 中保留这些稳定能力：

- 每个 epoch 组装 `EpochMetrics`
- 汇总 `CurrentFunds`
- 汇总 `CurrentEarn`
- 计算 `ParticipationRate`
- 计算 strongest competitor funds / earn
- 采样跨片交易并喂给 optimizer

这里迁移的是“数据流接口”，不是当前所有启发式调参。

### 5.5 CSV 基础结构

继续保留当前 CSV 的基础列和已稳定调试列：

- `epoch`
- `revenue`
- `broker_num`
- `mer`
- `fund`
- `Rank`
- `participation_rate`
- `current_investment`
- `sampled_cross_txs`
- `predicted_investment`
- `fund_share`
- `dominance_streak`
- `critical_mer_cap`
- `shock_exit_count`
- `shock_fund_drop`
- `optimizer_phase`

这些列已经与绘图脚本对齐，迁移后不应随意删改顺序。

### 5.6 绘图系统

直接迁移：

- `draw_diff_hub.py`
- `draw_diff_hub_common.py`
- `draw_diff_hub_extended.py`

要求：

- 保持兼容旧 CSV / 新 CSV / 任意 epoch 长度
- 保持输出 PDF 风格与文件名逻辑
- 保持当前默认命令可运行

## 6. 需要 clean reimplementation 的部分

以下部分不能机械复制当前代码，必须按“目标行为”重新实现：

### 6.1 `paper_monopoly` 的具体数值常数

当前仓库里的 `paper_monopoly` 已经被多轮调参影响。  
这些数值不应被视为规格：

- 各 phase 的目标 fee 常数
- 各种 wave amplitude / period
- dominance / shock / memory 的细节边界
- cap 上下界的具体比例

迁移时应只保留其目的：

- competition 期更偏向降费
- dominance 期更偏向小步提费
- shock 期捕捉骤然流失
- memory 期在 cap 下方活动

### 6.2 `committee_brokerhub.go` 中的故事塑形逻辑

当前文件里与下列内容相关的启发式，不要逐行照搬：

- phase bias
- `hubFlowMultiplier`
- `estimateHubUtility` 中的 stage 偏置
- `preferredMonopolyHub`
- `lockin` 阶段的人为优势/惩罚
- 为了让图更像预期而加的迁移节流与偏置

它们只能作为“你曾经试过这些控制方向”的参考。

### 6.3 当前为了出图取巧加入的 heuristic

只保留目标，不保留做法。  
例如：

- 人为让 loser 留一点 re-entry
- 人为控制 monopoly 形成时机
- 人为给 low-fee hub 加 flow boost

迁移后应尽量用更干净的机制表达。

## 7. `todolist_403.md` 中必须纳入迁移 prompt 的任务

这部分必须写进迁移 prompt，作为 fork 中的明确工作包。

### 7.1 费率机制与 broker 决策

必须保留为独立工作包：

- `Competition Phase`
- `Dominance Phase`
- `Shock Phase`
- `Memory Phase`
- `CriticalMERCap`
- broker 的 utility-based 选择逻辑

行为要求：

- broker 不是简单比 `hub revenue * (1 - fee)` 的缩放值
- 需要显式比较 direct utility 和 hub utility
- 当所有 Hub utility 均不为正时，应留在 B2E

### 7.2 Initial Allocation Asymmetry

必须作为下一阶段主任务保留：

- `allocation_alpha`
- `favored_hub_index`
- 两个 Hub 初始 seed fund 非对称分配

要求：

- 先做双 Hub 场景
- 不先强塞 broker 数量差异
- 优先在初始 seed funds 上制造 path dependence

### 7.3 alpha sweep 实验

必须写成未来脚本工作包：

- `run_alpha_sweep.py`
- `draw_alpha_sweep.py`
- `summary.json`
- `summary.csv`

要求：

- 支持 alpha 连续扫描
- 汇总 winner / loser / monopoly 形成时间 / tail MER / critical cap
- 支持挑出 special cases

### 7.4 验收标准

`todolist_403` 中的这些现象必须被写成验收目标：

- 非 monopoly 期能观察到 `MER ↓ -> fund ↑ -> revenue ↑`
- monopoly 形成期能观察到 `提费 -> broker 骤然流失 -> 记录 critical MER cap`
- monopoly 稳定期 winner 的 MER 在 cap 下方活动
- `alpha` 越小，favored hub 越容易成为最后赢家

## 8. 绘图系统迁移要求

绘图系统在迁移中必须被视为正式资产，而不是附属脚本。

### 8.1 三份脚本的角色

- `draw_diff_hub.py`
  - 主展示图
  - 负责表达 MER、participation、revenue ratio、broker revenue ratio 等主叙事

- `draw_diff_hub_common.py`
  - 共享读取和归一化层
  - 负责兼容旧 CSV、新 CSV、任意 epoch 数

- `draw_diff_hub_extended.py`
  - 扩展调试图
  - 负责展示 `sampled_cross_txs`、`current_investment`、`predicted_investment`、rank / broker count 等附加观察量

### 8.2 绘图兼容要求

迁移后必须继续满足：

- 旧 CSV 能画
- 新 CSV 能画
- 100 epoch、300 epoch、任意截断长度都能画
- 默认命令不变
- 输出 PDF 文件逻辑不变

### 8.3 文档资产

作为绘图参考文档保留：

- `python绘图脚本修改说明.md`
- `启动指南.md`

这两份文档进入参考文档组，不要求逐字迁移，但它们定义了当前使用方式和图的角色分工。

## 9. 不应迁移或只作为参考的内容

以下内容不应进入最终主线实现：

### 9.1 旧版 optimizer 实现

- `supervisor/optimizer/management_optimizer.go`

只作为参考，不作为主运行路径。

### 9.2 临时 update 文档和试错日志

以下内容不进入迁移主线：

- `update_331.md`
- 其他 update / changelog / 实验记录
- 当前仓库中为了调行为而留下的中间说明

这些文件只帮助理解过程，不帮助定义目标系统。

### 9.3 跑偏或不稳定的调参结果

当前仓库里为了修正图形效果做过多轮试错。  
这些试错结果不应被写成迁移规格，不应被复制到新仓库后继续沿用。

### 9.4 临时数值常数

凡是属于：

- 为了“让图更像”
- 为了“让 loser 不至于死太早”
- 为了“让 winner 不至于冲太高”

而引入的常数，都不应被作为最终正确实现。

## 10. 文件分组与迁移策略

### 10.1 直接迁移组

这些文件适合近似直接复制到 fork：

- `supervisor/optimizer/fee_optimizer.go`
- `supervisor/optimizer/tax_rate_optimizer.go`
- `supervisor/optimizer/tax_rate_optimizer_test.go`
- `params/fee_optimizer_mode.go`
- `params/fee_optimizer_mode_test.go`
- `params/exchange_mode.go`
- `params/exchange_mode_test.go`
- `draw_diff_hub.py`
- `draw_diff_hub_common.py`
- `draw_diff_hub_extended.py`

### 10.2 重写实现组

这些内容要按行为 clean reimplement：

- `supervisor/optimizer/paper_monopoly_optimizer.go`
- `supervisor/optimizer/paper_monopoly_optimizer_test.go`
- `committee_brokerhub.go` 中 monopoly shaping 相关逻辑
- `allocation_alpha` 接线
- alpha sweep 脚本
- alpha 绘图脚本

### 10.3 参考文档组

这些文件应作为参考材料提供给接手者：

- `todolist_403.md`
- `supervisor/optimizer/README.md`
- `python绘图脚本修改说明.md`
- `启动指南.md`
- `迁移/说明情况.md`

## 11. 建议实现顺序

为避免在 fork 中乱序开发，实施顺序固定如下：

1. 恢复基础参数与 optimizer 抽象层
2. 迁移 `taxrate` 方案与 committee 数据接线
3. clean reimplement `paper_monopoly`
4. 加入 broker utility 与 monopoly 观察量
5. 实现 `allocation_alpha`
6. 实现 alpha sweep 脚本与 alpha 绘图
7. 统一跑测试、300 epoch 实验与绘图验收

## 12. 验收标准

迁移后不得只以“能运行”为成功标准，必须满足下面四层验收：

### 12.1 代码层

- optimizer 测试通过
- committee 测试通过
- `taxrate` 和 `paper_monopoly` 都可正常启动

### 12.2 实验层

- 300 epoch 可跑通
- 能稳定生成 `hub0.csv` / `hub1.csv`
- CSV 中存在 `critical_mer_cap`、`optimizer_phase` 等调试列

### 12.3 绘图层

- `python draw_diff_hub.py` 成功
- `python draw_diff_hub_extended.py` 成功
- 后续 alpha 图脚本能沿用同一风格输出

### 12.4 行为层

必须至少观察到以下行为：

1. 非 monopoly 期存在片段：
   - `MER ↓`
   - `fund ↑`
   - `revenue` 在滞后几轮后上升

2. monopoly 形成期存在片段：
   - 领先 Hub 提费
   - broker / participation / fund 明显下跌
   - 记录 `critical_mer_cap`

3. monopoly 稳定期：
   - winner 的 `MER` 在 `critical_mer_cap` 下方活动
   - 不能是死平线
   - 也不能一路冲到 `0.99`

4. asymmetry 实验：
   - `alpha` 越小
   - favored hub 越容易成为稳定赢家
   - 结果在相同 seed 下可复现

## 13. 默认假设

- 当前阶段只产出迁移 prompt，不直接替新的 fork 仓库改代码
- fork 的本地路径未指定，因此默认以当前仓库为 source repo
- 迁移 prompt 面向另一个工程师或 Codex agent，默认使用中文
- 当前 `paper_monopoly` 不视为最终正确实现，只视为探索性原型
- 绘图脚本当前命令行入口和输出风格应尽量保持兼容

## 14. 实施提醒

在 fork 仓库中真正开始实现时，请遵守以下原则：

- 先稳定接口，再重做行为
- 先让 `taxrate` 跑通，再碰 `paper_monopoly`
- 不要先做 alpha sweep，再去补基础机制
- 不要把当前仓库里试错得到的硬编码常数直接复制过去
- 每完成一个阶段，都要用 CSV 和图验证是否仍然符合论文叙事与 `todolist_403` 要求
