### English

- Trae CN check-in no longer fails with 9074 on accounts added through the login flow: freshly generated device ids are 15 digits, the shape the claim gate accepts; 16-digit ids are refused with the misleading "too many users" message while the read-only status endpoint accepts any value.

### 中文

- Trae CN 签到不再对登录流程添加的账号报 9074：新生成的设备号为 15 位，即领取接口放行的形态；16 位号会被「当前参与用户太多」这一误导性文案拒绝，而只读的状态接口对设备号不校验。
