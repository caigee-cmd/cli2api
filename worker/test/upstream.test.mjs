import assert from "node:assert/strict";
import http from "node:http";
import { test } from "node:test";
import { fetchWithTimeout } from "../src/upstream.mjs";

function listen(handler) {
  return new Promise((resolve) => {
    const server = http.createServer(handler);
    server.listen(0, "127.0.0.1", () => resolve(server));
  });
}

function close(server) {
  return new Promise((resolve) => server.close(resolve));
}

// A head that never arrives must reject with first_byte_timeout so the Go side
// can fail over instead of the worker holding its in-flight slot forever.
test("first-byte deadline aborts a stuck head", async () => {
  const server = await listen(() => {
    /* accept the request, never respond */
  });
  try {
    const port = server.address().port;
    await assert.rejects(
      fetchWithTimeout(`http://127.0.0.1:${port}`, { method: "POST" }, { ttfbMs: 50 }),
      (err) => err.code === "first_byte_timeout",
    );
  } finally {
    await close(server);
  }
});

// A steady stream that keeps producing chunks must NOT be cut off, even when it
// runs longer than the stall window: the watchdog resets on every chunk.
test("stall watchdog does not truncate a steady stream", async () => {
  const server = await listen((req, res) => {
    res.writeHead(200, { "content-type": "text/event-stream" });
    let i = 0;
    const timer = setInterval(() => {
      res.write(`data: chunk-${i++}\n\n`);
      if (i >= 8) {
        clearInterval(timer);
        res.end();
      }
    }, 20);
  });
  try {
    const port = server.address().port;
    const resp = await fetchWithTimeout(`http://127.0.0.1:${port}`, {}, { ttfbMs: 500, stallMs: 100 });
    const reader = resp.body.getReader();
    let chunks = 0;
    while (true) {
      const { done } = await reader.read();
      if (done) break;
      chunks += 1;
    }
    // 8 interval writes over ~160ms with a 100ms stall window: a wall-clock cap
    // would have aborted it; a per-chunk reset lets it finish.
    assert.ok(chunks >= 1, `expected streamed chunks, got ${chunks}`);
  } finally {
    await close(server);
  }
});

// A stream that goes silent mid-body must reject with stream_total_timeout so a
// truncated upstream surfaces as a retryable timeout instead of a silent end.
test("stall watchdog aborts a silent mid-stream", async () => {
  const server = await listen((req, res) => {
    res.writeHead(200, { "content-type": "text/event-stream" });
    res.write("data: one\n\n");
    // then go silent forever
  });
  try {
    const port = server.address().port;
    const resp = await fetchWithTimeout(`http://127.0.0.1:${port}`, {}, { ttfbMs: 500, stallMs: 100 });
    const reader = resp.body.getReader();
    await reader.read(); // first chunk arrives
    await assert.rejects(reader.read(), (err) => err.code === "stream_total_timeout");
  } finally {
    await close(server);
  }
});
