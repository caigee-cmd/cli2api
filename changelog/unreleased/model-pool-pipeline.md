### English

- Adding a group shows what the two fields are for. The name is only for the console list. The client model ID is the model field in a request, and it cannot be changed after saving. The list and the editor say when a request enters the group: the model matches that ID with no provider prefix, the group is on, and either the system cross-provider pool or the key's aggregation switch is on.
- The menu item is a dispatch group. A request sits above one strategy, and seats line up left to right beneath it. A seat is one provider and one model. Leave accounts empty to use every connected account of that provider, or check several, in the order checked. Each seat card shows the model, the order, and the account names, with extra names as a count. Drag a seat to reorder it. Pan and zoom stay on the canvas. An intent branch is what splits into paths.
- Checking several accounts saves as every account of that provider, so the group can be saved now. Checking exactly one account still stores that account. The editor canvas uses the full width beside the sidebar.
- A sample message previews the path and the attempt order. A classifier model, when set, is called once for that preview; keyword matches stay local.
- Pools saved before this change open as the same member list. Saving keeps failover, weights, and the capabilities advertised on `/v1/models`.

### 中文

- 新增调度组时，两个输入会写明用途。名称只在控制台列表里辨认。客户端模型 ID 是请求里的 model，保存后不能改。列表和编辑页会写明请求何时进入这组：model 等于这个 ID 且不带供应商前缀，本组开着，并且系统跨 Provider 模型池或这把密钥的跨供应商聚合至少开一个。
- 菜单叫调度组。请求在上，下面是一种策略，座位从左到右排开。一个座位是一个供应商、一个模型。不选账号就用这个供应商已接入的全部账号，也可以勾上多个，顺序按勾选先后。座位卡片上能看到模型、账号数量、顺序和账号名字，多出来的用数字收起。座位可以拖动换位。平移和缩放只影响画布。意图分支才会再分成路径。
- 勾了多个账号时，保存按这个供应商的全部账号写入，所以现在可以保存。只勾一个账号时，仍只记那一个。编辑画布会用上侧栏旁边的整行宽度。
- 可以用一条样例消息预览命中的路径和尝试顺序。配置了分类模型时，预览会调用它一次；关键词匹配在本地完成。
- 这次改动之前保存的模型池会按原来的成员列表打开。保存后仍保留故障转移、权重，以及 `/v1/models` 上声明的能力。
