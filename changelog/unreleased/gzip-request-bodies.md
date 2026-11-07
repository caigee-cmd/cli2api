### English

- `POST /v1/chat/completions`, `/v1/messages` and `/v1/responses` accept `Content-Encoding: gzip` request bodies (`x-gzip` is accepted as its legacy alias). A long conversation compresses roughly 100-300x, which matters when the caller reaches the gateway through a proxy with a fixed response budget: uploading a multi-megabyte body can consume that budget before the origin starts working, and no server-side streaming can recover a request whose body never arrived. Decompressed size is bounded independently of the compressed size so a small gzip body cannot expand without limit. Requests without the header are read exactly as before, and an encoding the gateway cannot decode answers 415 with the encoding named instead of a JSON syntax error.

### 中文

- `POST /v1/chat/completions`、`/v1/messages` 与 `/v1/responses` 支持 `Content-Encoding: gzip` 请求体（`x-gzip` 作为旧别名一并接受）。长对话压缩比约 100-300 倍，这在客户端经由带固定响应预算的反向代理访问网关时很关键：上传几 MB 的请求体就可能耗尽该预算，而这部分时间服务端再怎么流式输出也救不回来。解压后的大小独立于压缩后大小单独限流，避免小体积 gzip 体无限膨胀。不带该头的请求读取方式完全不变；网关无法解码的编码返回 415 并指明编码名，而不是抛一个 JSON 语法错误。
