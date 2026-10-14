### English

- Keep quota-exhausted accounts routable for free models. Qoder prices some catalog models at zero (`limited_time_free` tag or a zero `price_factor`), and requests for them spend no credits, but the routing pool filtered every quota-exceeded account unconditionally — leaving those accounts' free models with no account at all. The pool now records which catalog entries the provider prices at zero alongside the cached model list, and the quota filter exempts a request whose model is on that list. Pricing-unknown models are not assumed free, and the exemption is scoped per model, so exhausted accounts stay out of paid routes.

### 中文

- 额度耗尽的账号现在仍可路由到免费模型。Qoder 目录中部分模型单价为零（`limited_time_free` 标签或 `price_factor` 为零），请求不消耗积分，但路由池原先无条件排除所有额度耗尽的账号，导致这些账号的免费模型无账号可用。路由池现在在缓存模型列表的同时记录上游报价为零的模型，额度过滤对命中该名单的请求予以豁免。未报价的模型不视为免费；豁免按请求模型限定，额度耗尽的账号依旧不会出现在付费模型的路由上。
