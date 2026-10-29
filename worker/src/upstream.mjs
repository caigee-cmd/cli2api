// Upstream fetch with a first-byte deadline and a mid-stream stall watchdog.
// Extracted from daemon.mjs so it can be unit-tested without booting the CLI.

// First-byte and mid-stream stall deadlines protect the worker from a stuck
// upstream: Qoder's SSE can hang at the head (TTFB) or stop sending data
// mid-stream when the gateway is degraded. The TTFB timer stops as soon as the
// response head arrives. The stall watchdog is NOT a wall-clock cap: it resets
// every time a chunk arrives, so a long thinking-model stream that keeps
// producing output is never truncated — only a stream that goes silent for the
// whole window is aborted. The TTFB default mirrors the Go-side first-byte
// deadline so worker and gateway agree on when a stuck head counts as a
// failure; both are tunable per deployment.
export const streamTtfbMs = Math.max(5_000, Number(process.env.QODER_STREAM_TTFB_MS || 60_000) || 60_000);
export const streamStallMs = Math.max(streamTtfbMs, Number(process.env.QODER_STREAM_TOTAL_MS || 300_000) || 300_000);

// fetchWithTimeout wraps fetch with a TTFB deadline on the response head and
// (optionally) a mid-stream stall watchdog on the body. The TTFB timer fires
// only if the head has not arrived; once it does, the timer is cleared. The
// stall watchdog is attached to the body and only fires when no chunk has
// arrived for stallMs. When a deadline fires the rejection carries a stable
// `.code` (first_byte_timeout / stream_total_timeout) that the callers forward
// so the Go side can classify and fail over instead of seeing a bare abort.
export async function fetchWithTimeout(url, opts = {}, { ttfbMs = streamTtfbMs, stallMs = 0 } = {}) {
  const controller = new AbortController();
  const abortWith = (code, message) => {
    const err = new Error(message);
    err.code = code;
    controller.abort(err);
  };
  const ttfbTimer = setTimeout(() => {
    abortWith("first_byte_timeout", `upstream first-byte timeout after ${ttfbMs}ms`);
  }, ttfbMs);
  let resp;
  try {
    resp = await fetch(url, { ...opts, signal: controller.signal });
  } catch (err) {
    clearTimeout(ttfbTimer);
    // err carries .code when our own timer aborted; rethrow unchanged.
    throw err;
  }
  clearTimeout(ttfbTimer);
  if (stallMs > 0 && resp.body) attachStallWatchdog(resp, controller, stallMs);
  return resp;
}

// attachStallWatchdog replaces resp.body with a wrapper that arms a timer on
// each chunk and aborts the request when the stream goes silent for stallMs.
// The timer is cleared when the body ends, errors, or is cancelled, so it never
// outlives the request. Aborting the controller mid-stream makes the reader
// reject with the abort reason (code=stream_total_timeout), which the SSE pipe
// turns into a structured error event rather than a silent truncation.
export function attachStallWatchdog(resp, controller, stallMs) {
  const original = resp.body;
  if (!original) return;
  const reader = original.getReader();
  let timer = null;
  const disarm = () => {
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
  };
  const arm = () => {
    disarm();
    timer = setTimeout(() => {
      const err = new Error(`upstream stream stalled for ${stallMs}ms`);
      err.code = "stream_total_timeout";
      controller.abort(err);
    }, stallMs);
    if (typeof timer.unref === "function") timer.unref();
  };
  arm();
  const wrapped = new ReadableStream({
    async pull(ctrl) {
      try {
        const { done, value } = await reader.read();
        if (done) {
          disarm();
          ctrl.close();
          return;
        }
        arm();
        ctrl.enqueue(value);
      } catch (err) {
        disarm();
        ctrl.error(err);
      }
    },
    cancel(reason) {
      disarm();
      return reader.cancel(reason);
    },
  });
  Object.defineProperty(resp, "body", { value: wrapped, configurable: true });
}
