# M1.7外派提示词—Adversarial DONE-Integrity Qualification

> 贴给负责实现的ChatGPT会话。会话拥有GitHub connector用于提交和开PR，拥有一个断网执行沙箱；构建和测试必须在附带的tarball工作树中完成。tarball是权威基线，connector侧SHA不同是正常现象。

---

## 你的任务

严格按仓库中的spec和implementation plan实现M1.7：

    docs/superpowers/specs/2026-08-30-m1-7-done-integrity-qualification-design.md
    docs/superpowers/plans/2026-08-30-m1-7-done-integrity-qualification.md

目标是为docs/M1-DESIGN.md§4.4定义的四类DONE-integrity攻击建立证据：

- S1–S3：用live kernel和外部身份local-cli执行；
- S4：不伪造M1不存在的live路径，而是用真实的runPipeline → doneGate集成缝测试加现有gate谓词测试；
- 不声称覆盖所有可能的bypass路径；
- 不新增任何生产enforcement代码。

计划按Task1到Task6顺序执行。Task1到Task5各自按计划提交一次；Task6只做最终验收、致残对照和交付记录。

---

## 开工前：基线和BASE

tarball解压后立即执行：

    git log --oneline
    git status --porcelain
    BASE="$(git rev-parse HEAD)"
    export BASE
    git switch -c "chatgpt/m1-7-done-integrity-qualification"

BASE必须由当前tarball的HEAD动态捕获，禁止写死SHA。之后所有G1检查使用"$BASE"。当前分支应是干净的单提交handoff snapshot；不要从connector侧历史补文件。

基线检查：

    go build "./plugins/..." "./cli/..."
    ( cd "kernel" && go build "./..." )
    rm -f "./vibe"
    bash "scripts/check-arch.sh"

基线编译失败、git status --porcelain有非残留改动、或当前HEAD不是tarball快照时，停止并报告。check-arch.sh的contract/manifest数字以实际输出为准；M1.7不修改它。

---

## 前置处理协议：检查—自修—升级

遇到边角问题先按以下顺序处理，不要因为可恢复问题停车：

1. safe.directory或.DS_Store残留：直接修复或删除。
2. 数字与计划字面不符：信命令输出，调整记录，不改生产文件迁就数字。
3. 轮询或慢沙箱导致S3在review创建前触发20秒deadline：按计划把该S3块改为40s，记录degradation；不要改成固定sleep。
4. 计划命令的shell笔误：保持原意直接修正，并记录偏离。
5. jsonschema或其他可选工具缺失：按计划降级并记录，不伪造通过结果。

只有下列情况停车并报告：

- 需要修改kernel任何路径；
- 需要修改docs/M1-DESIGN.md或config/m1-bindings.json、contracts、manifests；
- 需要放宽DONE gate、给local-cli直接授予work.transition@1、增加生产mock flag或引入新的外部Go/Python依赖；
- M0.5 qualification变红；
- 致残对照不变红，说明测试没有真正咬住防线；
- 无法在不改变上述约束的情况下完成计划。

---

## 执行纪律

每个任务必须遵循：

1. 先写测试或可观察的失败检查；
2. 运行并确认是预期原因失败；
3. 写最小实现；
4. 运行相关测试变绿；
5. 行为性改动按计划做一次致残对照，确认变红后立即恢复；
6. 按计划给定的commit message提交，并保留commit trailer。

必须遵守：

- 共享kernel生命周期只放入scripts/lib/kernel-harness.sh，scripts/smoke.sh只做无行为变化的抽取改造；
- qualification脚本的fail()必须输出消息、kernel.log尾40行并以1退出；
- 所有会影响验收结论的命令都必须fail-closed；不能让后续echo覆盖失败码；
- 禁止cmd | grep -q和grep -o ... | head -1；首个匹配使用grep -m1 -o；
- S3必须通过.bin/vibe-raw调用review.query，验证正好两条review，并验证R_fake为APPROVED、R_real为PENDING；
- S4测试必须驱动真实runPipeline，断言stale diff得到GATE_FAILED、没有transition:DONE，并保留cleanup断言；
- S4在脚本中的标记使用S4 SEAM OK，不能把没有执行live攻击的指针行写成live通过；
- 所有临时policy或生产代码致残修改必须在同一步恢复，最终git status --porcelain为空；
- 所有变量路径加双引号；固定literal路径遵循计划中与仓库现有脚本一致的写法。

允许修改的最终文件只有：

    plugins/engineering-workflow/pipeline_test.go
    scripts/lib/kernel-harness.sh
    scripts/qualify-done-integrity.sh
    scripts/smoke.sh

kernel、docs/M1-DESIGN.md、scripts/check-arch.sh和其他生产代码不得出现在最终diff中。

---

## 预算耗尽协议

如果工具或会话预算接近耗尽：

- 只在Task边界停，不停在半个任务中；
- 先把已完成任务的commit通过connector推送并推进分支ref；
- 报告最后一个已推送的commitSHA和下一个尚未开始的任务号；
- 没有跑过的测试必须明确写“未运行”，禁止伪造输出；
- Task6之前不能把未完成状态描述成M1.7完成。

---

## 最终验收和PR

按计划Task6逐步执行并把原始输出放入PR，不要只写摘要：

1. 三模块build；
2. plugins、CLI和kernel全量Go测试，并确认TestRunPipelineGateFailOnStaleDiff通过；
3. 已有kernel的m05_qualification.py回归；
4. 未修改的check-arch.sh；
5. bash "scripts/qualify-done-integrity.sh"运行3次：S1–S3 live标记、S4 seam标记、最终成功标记和0孤儿进程；
6. bash "scripts/smoke.sh"运行5次：每次workflow smoke、M1 smoke和0 FAIL，最终再次检查0孤儿进程；
7. 用"$BASE"执行G1和完整diff白名单检查；
8. 逐项执行M-S1到M-S4致残对照：每项变红、恢复后变绿，并记录精确红文案。

PR正文必须明确：

> S1–S3由live kernel+local-cli验证；S4由runPipeline → doneGate集成缝和gate谓词验证。四项对应§4.4定义的攻击，不代表所有bypass路径都已证明关闭。

PR分支：

    chatgpt/m1-7-done-integrity-qualification → main

PR标题：

    M1.7—Adversarial DONE-Integrity Qualification

正文包含Task1到Task5的commit表、Task6原始输出、所有degradation/偏离、M-S1到M-S4结果，以及“reviewer会独立重跑”的说明。自报不构成验收。
