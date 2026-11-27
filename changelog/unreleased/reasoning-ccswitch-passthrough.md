### English

- Reasoning effort now follows the CC Switch rule end to end: a level the client sends is passed through untouched whenever the model declares it, an undeclared level only degrades to the nearest lower declared level, and it is never promoted to the model default — including the WorkBuddy "none" path that used to fall back to the default tier.

### 中文

- 思考强度按 CC Switch 规则处理：客户端传来的档位只要模型声明过就原样透传，未声明时只向下退到最近的已声明档位，绝不再被提升为模型默认档——WorkBuddy 原先回落到默认档的「none」分支也一并修正。
