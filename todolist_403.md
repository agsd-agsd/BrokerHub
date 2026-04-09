# 1.两个hub竞争过程的预期效果
---
## 1.revenue
如果只观察revenue的公式，一个Hub降低管理费率是无法判断revenue是增加还是减少的，因为Hub的管理费率降低了，但同时Hub的AUM可能增加了，但是从论文的角度出发，我们预期的效果是：在未达到monopoly的情况下，一个Hub降低管理费率会增加revenue，因为它会吸引更多的客户和资金，从而增加AUM，虽然管理费率降低了，但AUM的增加可能会抵消管理费率的降低，最终导致revenue的增加。我需要你想办法实现这个效果

## 2.monopoly情况分析
一个hub在取得垄断优势的时候，会倾向于增加管理费率来最大化利润，因为在垄断市场中，消费者没有其他选择，只能接受这个hub的服务，因此hub可以通过提高管理费率来增加收入。然而，当broker意识到这个hub的垄断优势时，他们可能会选择离开这个hub，寻找其他的投资机会，并且这个变化通常是骤然性的，因为一旦消费者意识到这个hub的垄断优势，他们可能会迅速做出反应。因此，在这种情况下，我们预期的效果是：当一个hub取得垄断优势时，它可能会增加管理费率来最大化利润，但这可能会导致消费者的骤然流失。我们记录下这个管理费率的临界值，并作为这个hub以后的最高管理费率的参考。同理的，另外一个hub获得大量的broker也会倾向于增加管理费率来最大化利润，但同样会面临消费者的骤然流失。因此，在这种情况下，我们预期的效果是：当另一个hub获得大量的broker时，它可能会增加管理费率来最大化利润，但这可能会导致消费者的骤然流失。我们记录下这个管理费率的临界值，并作为这个hub以后的最高管理费率的参考。到目前为止，从宏观上看，并没有任何一个hub实现持续性的垄断。但是在之后的epoch中，我们会观察到一个hub参考之前的最高阈值，从而实现了持续性的垄断。

# 2.对论文中Initial Allocation Asymmetry的实现
请分析origin\LiquidityPool__camera-ready.pdf中4.2节中Initial Allocation Asymmetry的实现，并且计划如何在我们的模拟中实现这个效果。

# 3.基于Initial Allocation Asymmetry的拓展
Initial Allocation Asymmetry是指在初始阶段，某些hub可能会获得更多的客户和资金，而其他hub则相对较少。这种不对称的分配可能会导致市场竞争的不平衡，从而影响整个市场的效率和公平性。我们需要研究alpha这个决定性参数对于实验结果的影响。我需要你想办法实现对连续的alpha值的分析分析测试，并且写一个python绘制脚本来展现alpha值对于实验结果的影响。同时，你也需要研究特殊的alpha值所造成的实验现象。

---

# 4.后续实现计划（Codex补充）

## 4.1 总体目标
下一阶段不再只看“费率会不会波动”，而是明确把 BrokerHub 调成更接近论文叙事的三段式结果：

1. 在非 monopoly 阶段，Hub 降低 MER 后能够通过吸引更多 broker 和 AUM，在若干个 epoch 内带来更高 revenue。
2. 在 monopoly 形成阶段，领先 Hub 会尝试逐步提高 MER，并在越过某个阈值后触发 broker 的骤然流失。
3. 在引入 Initial Allocation Asymmetry 后，系统会随着 `alpha` 下降更容易进入稳定 monopoly，且 monopoly 身份由初始分配决定。

## 4.2 费率机制与 broker 决策的实现计划

### A. 先把当前 `paper_monopoly` 从“单纯调费”升级成“带阈值记忆的 monopoly 控制器”
实现位置以 `supervisor/optimizer/paper_monopoly_optimizer.go` 为主，保留 `taxrate` 和现有 `paper_monopoly` 的主干，不新建第三套无关模式。

需要新增的状态：

- `DominanceStreak`
- `FundShareHistory`
- `ParticipationHistory`
- `CriticalMERCap`
- `CriticalMEREpoch`
- `LastShockExitCount`
- `LastShockFundDrop`
- `HasCriticalMERCap`

新的费率阶段划分：

1. `Competition Phase`
   - 条件：`fund_share < 0.8` 且 `participation_rate < 0.8`
   - 行为：优先允许降费抢 AUM，而不是优先提费
   - 调整依据：
     - 自己和最强对手的资金差
     - 最近 `k` 轮 revenue 斜率
     - 最近 `k` 轮 invested funds 斜率
     - 最近一次降费后是否出现“资金涨、收益也涨”
   - 目标效果：让“降 MER -> AUM 增 -> revenue 增”的路径在竞争期更常出现

2. `Dominance Phase`
   - 条件：`fund_share >= 0.8` 或 `participation_rate >= 0.8` 连续 `N` 轮
   - 行为：允许小步提高 MER，逼近利润最大化
   - 调整原则：
     - 单轮增幅固定限制在一个很小范围内，避免直接冲顶
     - 每次上调后必须观察至少 1 个 epoch 的 broker / fund 反馈

3. `Shock Phase`
   - 条件：领先 Hub 提费后，下一轮或两轮内出现明显流失
   - 判定信号固定为以下任一满足：
     - `participation_rate` 单轮下降 >= `0.2`
     - `broker_num` 单轮下降 >= `max(3, 15%)`
     - `invested_funds` 单轮下降 >= `15%`
   - 行为：
     - 记录触发流失前一轮的 MER 为 `CriticalMERCap`
     - 当前 MER 立即回落到 `CriticalMERCap * 0.9`
     - 后续上界固定为 `min(dynamicUpperBound, CriticalMERCap * 0.95)`

4. `Memory Phase`
   - 一旦某个 Hub 记录过 `CriticalMERCap`，之后都不允许长期超过这个阈值
   - 该阈值只允许缓慢下调，不允许重新抬高
   - 目标是形成“Hub 记住历史踩坑费率”的效果

### B. broker 决策逻辑改成更接近论文 Algorithm 2
当前 `committee_brokerhub.go` 中的 broker 决策仍然偏启发式，只是比较 B2E 收益和 `hub_revenue * (1 - fee)` 的缩放值。下一步要改成论文里更接近的 utility 差值判断。

具体调整：

1. 对每个 broker 计算 `direct utility`
   - 已有历史收益的 broker：直接用最近一轮或最近几轮平均 B2E 收益
   - 新 broker：按论文思路，用资金规模最接近的两个 broker 做加权插值估计

2. 对每个 Hub 计算 `hub utility`
   - 使用论文中的 `h(v, δ_b)` 结构：
     - `hub gross revenue per unit` 来自当前 hub 收益 / 当前 hub AUM
     - broker 拿到的比例使用 `(1 - MER)`
     - 再减去 direct utility

3. 决策规则固定为：
   - 若所有 Hub 的 `h(v, δ_b) <= 0`，则留在 B2E
   - 若存在正值，则加入 `argmax h(v, δ_b)` 的 Hub
   - 若处于平局，维持当前状态，不来回横跳

4. 保留离散 broker 和 all-or-nothing 迁移
   - 不做部分分配
   - 这样可以保留论文里“阈值一过，资金和 broker 会骤然迁移”的特征

### C. 为了观察上述过程，CSV 和日志要补调试量
在不破坏现有绘图脚本读取的前提下，现有 CSV 列后面继续追加：

- `fund_share`
- `dominance_streak`
- `critical_mer_cap`
- `shock_exit_count`
- `shock_fund_drop`
- `optimizer_phase`

同时在 `Supervisor.log` 中按 epoch 打印：

- 当前 phase
- 当前 MER
- strongest competitor funds / earn
- 是否触发 shock
- 是否更新 critical MER cap

### D. 这一阶段的验收标准

1. 非 monopoly 时，至少能在多个样本片段中看到：
   - `MER` 下降后 1-3 个 epoch 内 `fund` 上升
   - `fund` 上升后 `revenue` 也上升

2. monopoly 形成期，至少一侧会出现：
   - `participation_rate >= 0.9` 持续若干轮
   - 之后继续提费时出现一次明显 broker 流失
   - CSV 中留下 `critical_mer_cap`

3. monopoly 稳定期，领先 Hub 的 MER 会在 `critical_mer_cap` 下方收敛，而不是一直冲到 0.99

## 4.3 Initial Allocation Asymmetry 的实现计划

### A. 论文中 `alpha` 的实现含义
根据论文 4.2 节：

- `alpha = 1` 表示完全对称
- `alpha -> 0` 表示强烈不对称
- 当 `alpha < alpha*` 时，系统更容易进入 monopoly
- monopoly 的赢家由初始分配决定，而不是单纯由后续操作决定

### B. 在 BrokerHub 中的具体落地方式
下一阶段不把 `alpha` 用在 B2E 算法里，而是专门新增一套“初始 AUM 不对称”参数，避免和 `Broker2Earn/B2E.go` 内部那个局部 `alpha := 1` 混淆。

新增运行参数：

- `--allocation_alpha`
  - `float64`
  - 默认 `1.0`
- `--favored_hub_index`
  - `int`
  - 默认 `0`

参数语义：

- `allocation_alpha = 1.0`
  - 两个 Hub 初始 AUM 完全对称
- `allocation_alpha < 1.0`
  - `favored_hub_index` 指向的 Hub 在 epoch 0 拿到更高的初始 AUM

### C. 初始 AUM 映射规则固定为两 Hub 版本
为了直接对应论文里的“赢家由初始 allocation 决定”，先只做两 Hub 场景，映射规则固定为：

- `favored_share = 1 / (1 + alpha)`
- `other_share = alpha / (1 + alpha)`

这样有三个好处：

1. `alpha = 1` 时正好 `0.5 / 0.5`
2. `alpha` 越小，优势 Hub 占比越高
3. 两个 share 总和恒为 `1`

### D. 初始不对称先落在 Hub seed funds，而不是先改 broker 数
第一版实现只改 `init_brokerhub()` 中两个 Hub 的初始资金分配，不预先把 broker 强行塞进某个 Hub。

原因：

1. 论文 4.2 讨论的核心是 `D_B(b, δ_0)` 的初始 allocation，不是初始 broker 计数本身
2. 当前 BrokerHub 的 broker 会在第一个 epoch 后根据收益自己迁移，初始 seed AUM 已足够制造 path dependence
3. 这样实现最干净，不会把“初始不对称”和“broker 决策逻辑变化”混在一起

具体改法：

1. `committee_brokerhub.go:init_brokerhub()`
   - 不再把两个 Hub 都初始化为完全相同的 `Init_broker_Balance * ShardNum`
   - 改为先计算两个 Hub 的总初始 seed funds，再按上述 share 分配

2. `newFeeOptimizer(...)`
   - 继续传各自真实 `initialFunds`
   - 不能再假设两个 Hub 的 `InitialFunds` 相同

3. CSV 新增一列
   - `initial_fund_share`
   - 用于后续校验初始不对称是否真的生效

### E. 这一阶段的验收标准

1. `alpha = 1.0`
   - 两个 Hub 初始 `fund` 基本相同

2. `alpha = 0.5`
   - favored hub 初始 `fund` 约为另一侧的 `2x`

3. `alpha = 0.2`
   - favored hub 从早期 epoch 就更容易形成持续领先

4. 相同 seed 下，favored hub 不变时，赢家身份应稳定复现

## 4.4 连续 alpha 分析与绘图计划

### A. 新增批量实验脚本
新增一个 Python 批量脚本，例如：

- `run_alpha_sweep.py`

功能固定为：

1. 接收：
   - `alpha_start`
   - `alpha_end`
   - `alpha_step`
   - `seed`
   - `epochs`
   - `favored_hub_index`

2. 对每个 alpha：
   - 生成一次运行命令
   - 执行一轮实验
   - 将输出保存到独立目录

建议目录结构：

- `./alpha_sweep/alpha_1.00/`
- `./alpha_sweep/alpha_0.90/`
- `./alpha_sweep/alpha_0.80/`

每个目录至少保存：

- `hub0.csv`
- `hub1.csv`
- `Supervisor.log`
- `summary.json`

### B. 每个 alpha 的汇总指标固定输出到 `summary.csv`
汇总字段固定包含：

- `alpha`
- `favored_hub_index`
- `winner_hub`
- `winner_fund_share_tail_mean`
- `winner_participation_tail_mean`
- `loser_participation_tail_mean`
- `first_monopoly_epoch`
- `stable_monopoly_epoch`
- `tail_mer_mean_winner`
- `tail_mer_mean_loser`
- `critical_mer_cap_winner`
- `critical_mer_cap_loser`
- `monopoly_formed`

其中 monopoly 判定标准固定为：

- 最后 50 个 epoch 中
- 至少 40 个 epoch 满足一侧 `participation_rate >= 0.9`
- 且另一侧 `participation_rate <= 0.1`

### C. 连续 alpha 取值方案
第一版分析采用两段式采样：

1. 全局扫描
   - `alpha = 1.00, 0.95, 0.90, ..., 0.10`

2. 临界区细扫
   - 根据第一次全局扫描结果，在 monopoly 形成概率从低到高突变的区间再做一次 `0.02` 或 `0.01` 步长细扫

### D. 新增 alpha 绘图脚本
新增一个主绘图脚本，例如：

- `draw_alpha_sweep.py`

风格保持和现有 BrokerHub 绘图脚本一致，输出一张多子图 PDF。

建议固定成 2x3 布局：

1. `alpha vs stable monopoly rate`
2. `alpha vs first monopoly epoch`
3. `alpha vs winner tail fund share`
4. `alpha vs winner tail MER`
5. `alpha vs critical MER cap`
6. `selected alpha trajectories`
   - 选择 `alpha = 1.0 / 0.8 / 0.6 / 0.4 / 0.2`
   - 在同一张子图中展示 winner/loser 的 `participation_rate` 或 `fund share` 演化

### E. 特殊 alpha 现象分析
“特殊 alpha” 不只指极端值，也包括临界值附近。

需要重点找三类：

1. `alpha ≈ 1`
   - 竞争长期拉扯，难以形成稳定 monopoly

2. `alpha ≈ alpha*`
   - 轻微改动就会导致结果从“长期竞争”切到“稳定 monopoly”
   - 这是最重要的临界现象

3. `alpha` 很小
   - favored hub 很快锁定 monopoly
   - loser 基本失去恢复空间

批量脚本需要自动把这三类 alpha 的轨迹单独复制到：

- `./alpha_sweep/special_cases/`

供后续人工写分析结论。

## 4.5 测试与验收计划

### A. Go 单测

1. `paper_monopoly_optimizer_test.go`
   - dominance phase 进入条件
   - shock phase 触发条件
   - `CriticalMERCap` 记录逻辑
   - 记录后 upper bound 不得超过安全上限

2. `committee_brokerhub_test.go`
   - `allocation_alpha = 1.0` 时两个 Hub seed funds 对称
   - `allocation_alpha < 1.0` 时 favored hub 初始资金更高
   - broker utility 决策会在 fee 跨阈值后转向退出

### B. Python 侧 smoke test

1. `run_alpha_sweep.py`
   - 跑两个 alpha 值的最小样例，确认目录、summary 输出正确

2. `draw_alpha_sweep.py`
   - 读取最小样例 summary，确认能正常出图

### C. 最终实验验收

需要至少完成三组代表性实验：

1. `alpha = 1.0`
   - 观察是否仍然偏长期竞争或 knife-edge 摇摆

2. `alpha = 0.5`
   - 观察是否开始稳定偏向 favored hub

3. `alpha = 0.2`
   - 观察是否较快收敛到 monopoly

最终只要满足下面三条，就说明这一轮实现成功：

1. 竞争期能看到“降 MER 换更高 revenue”的片段
2. monopoly 形成期能捕捉到一次“提费过头 -> broker 骤然流失 -> 记录 critical MER cap”
3. `alpha` 越小，favored hub 越容易成为最后赢家，且结果可重复
