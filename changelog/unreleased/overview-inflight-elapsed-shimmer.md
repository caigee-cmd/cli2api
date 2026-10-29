### English

- Overview InflightLiveCard busy slots show **real elapsed** seconds from pool `InFlightSince` → `/api/live` `workers[].started_at` (client ticks ~150ms; falls back to first-seen busy time if the field is absent). Progress bar is an **indeterminate** solid-glass shimmer — no fake % toward completion. Idle stays 空闲; rate-limit amber unchanged; GSAP fly in/out kept.

### 中文

- Overview「实时进行中」忙碌槽位显示真实已耗时（Pool `InFlightSince` → `/api/live` `started_at`，前端约 150ms 滴答；无字段时用首次见到忙碌的时刻）。进度条改为不确定流光，不再用假百分比。空闲仍为「空闲」；限流琥珀不变；GSAP 飞入/飞出保留。
