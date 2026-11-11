package gateway

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Compressed request bodies.
//
// A long chat request is mostly redundant: the same system prompt, the same tool
// schemas, and the same indentation repeated across every message. A real 60-turn
// history compresses roughly 100-300x with plain gzip, which matters when the
// caller reaches the gateway through a proxy with a fixed response budget
// (Cloudflare's 120s Proxy Read Timeout is not configurable below Enterprise):
// uploading a multi-megabyte body can consume that budget before the origin even
// starts working, and no amount of upstream-side streaming can recover a request
// whose body never arrived.
//
// Decoding is strictly opt-in on the caller: without a Content-Encoding header
// the body is read exactly as before, so clients that never compress see no
// behaviour change at all.
//
// Two limits are enforced independently:
//   - the wire limit counts compressed bytes, bounding how much we read off the
//     socket before deciding the request is too large;
//   - the inflated limit counts decompressed bytes, which is the actual memory
//     the JSON decoder will touch. Without it a small gzip body could expand to
//     gigabytes (a "zip bomb") and pin the process.
//
// An unknown encoding is rejected rather than ignored: silently treating a zstd
// body as JSON would produce a confusing syntax error instead of naming the
// unsupported encoding.

// ErrUnsupportedContentEncoding reports a Content-Encoding the gateway cannot
// decode. Callers turn it into a 415.
var ErrUnsupportedContentEncoding = errors.New("unsupported content encoding")

// ErrInflatedBodyTooLarge is returned when a decompressed body exceeds the
// inflated limit. Unlike io.LimitReader, this is a hard reject: the caller
// must not continue with a truncated body.
var ErrInflatedBodyTooLarge = errors.New("decompressed request body too large")

// decodeRequestBody returns a reader for the request body with any supported
// Content-Encoding removed.
//
// The returned reader must be closed by the caller only when it is not the raw
// body; requestBodyReader handles that.
func decodeRequestBody(w http.ResponseWriter, r *http.Request, wireLimit, inflatedLimit int64) requestBodyReader {
	body := http.MaxBytesReader(w, r.Body, wireLimit)
	encoding := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding")))
	// A client may send a preference list; only a single applied encoding is
	// meaningful on a request, so anything else is rejected below.
	switch encoding {
	case "", "identity":
		return requestBodyReader{Reader: body, raw: body}
	case "gzip", "x-gzip":
		// The gzip header itself is tiny, but read it through the same wire
		// limiter so a hostile client cannot stream megabytes of compressed
		// garbage into the decompressor's input buffer.
		gz, err := gzip.NewReader(body)
		if err != nil {
			return requestBodyReader{err: fmt.Errorf("invalid gzip body: %w", err), raw: body}
		}
		return requestBodyReader{
			Reader: &inflatedLimitReader{r: gz, n: inflatedLimit},
			gz:     gz,
			raw:    body,
		}
	default:
		return requestBodyReader{err: fmt.Errorf("%w: %s", ErrUnsupportedContentEncoding, encoding), raw: body}
	}
}

// inflatedLimitReader bounds decompressed reads the way http.MaxBytesReader
// bounds the wire: reading past the limit returns a distinct error instead of
// a silent EOF, so the JSON decoder cannot treat a truncated object as a
// syntax typo or, worse, succeed on a prefix that happens to be valid JSON.
type inflatedLimitReader struct {
	r io.Reader
	n int64
}

func (r *inflatedLimitReader) Read(p []byte) (int, error) {
	if r.n < 0 {
		return 0, ErrInflatedBodyTooLarge
	}
	if r.n == 0 {
		return r.rejectIfMore()
	}
	if int64(len(p)) > r.n {
		p = p[:r.n]
	}
	n, err := r.r.Read(p)
	r.n -= int64(n)
	if r.n == 0 && err == nil {
		if _, peekErr := r.rejectIfMore(); peekErr != nil {
			return n, peekErr
		}
	}
	return n, err
}

func (r *inflatedLimitReader) rejectIfMore() (int, error) {
	var buf [1]byte
	n, err := r.r.Read(buf[:])
	if n > 0 {
		r.n = -1
		return 0, ErrInflatedBodyTooLarge
	}
	if err == nil {
		return 0, nil
	}
	return 0, err
}

// requestBodyReader is the decoded body plus the handles needed to release it
// without leaking the gzip reader.
type requestBodyReader struct {
	io.Reader
	gz  *gzip.Reader
	raw io.Closer
	err error
}

// Close releases the gzip reader and the underlying body.
func (b requestBodyReader) Close() error {
	var firstErr error
	if b.gz != nil {
		// Close on the gzip reader does not close the underlying reader, so
		// both are closed. A decode error in the trailer is not worth reporting
		// here: the body was already consumed by the caller.
		if err := b.gz.Close(); err != nil && !errors.Is(err, gzip.ErrChecksum) {
			firstErr = err
		}
	}
	if b.raw != nil {
		if err := b.raw.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// requestBodyCompressed reports whether the request carried a decoded encoding,
// for logging and diagnostics only.
func requestBodyCompressed(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))) {
	case "gzip", "x-gzip":
		return true
	default:
		return false
	}
}

func isRequestBodyTooLarge(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrInflatedBodyTooLarge) {
		return true
	}
	var maxBytes *http.MaxBytesError
	return errors.As(err, &maxBytes)
}

// writeRequestBodyError turns a body-decoding failure into the OpenAI error
// shape. An encoding the gateway cannot decode is a client contract problem
// (415), not a malformed request; an over-limit body is 413; everything else
// is the existing 400 path so callers keep the message they had before.
func writeRequestBodyError(w http.ResponseWriter, err error) {
	status, code, msg := classifyRequestBodyError(err)
	writeErr(w, status, code, msg)
}

// writeAnthropicRequestBodyError is the /v1/messages counterpart: same status
// and message, but the Anthropic error envelope this endpoint already uses.
func writeAnthropicRequestBodyError(w http.ResponseWriter, err error) {
	status, _, msg := classifyRequestBodyError(err)
	writeAnthropicError(w, status, "invalid_request_error", msg)
}

func classifyRequestBodyError(err error) (status int, code, msg string) {
	if errors.Is(err, ErrUnsupportedContentEncoding) {
		return http.StatusUnsupportedMediaType, "unsupported_content_encoding", err.Error()
	}
	if isRequestBodyTooLarge(err) {
		return http.StatusRequestEntityTooLarge, "invalid_request", "request body too large"
	}
	return http.StatusBadRequest, "invalid_request", err.Error()
}
