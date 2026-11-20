package doubaowork

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
)

// Temporary conversations
//
// Every chat request creates a throwaway upstream conversation
// (conversation_type=3). If it is not deleted, the user's Doubao sidebar fills
// with unknown chats. The lease records the local conversation id, claims the
// real conversation id from SSE_ACK, and deletes it when the request finishes.
//
// Bookkeeping is process-internal: it lives as long as the adapter process.
// Cleanup is best-effort; a failure is recorded for a later attempt but never
// turns a delivered reply into an error. If the process is killed mid-request an
// orphan conversation can remain and the user removes it in Doubao.

const (
	imRecentConvPath = "/im/chain/recent_conv"
	imDeleteConvPath = "/im/conversation/batch_del_user_conv"
	imDeleteConvCmd  = 4171
	conversationType = 3
)

type lease struct {
	session        *session
	localID        string
	bot            string
	conversationID string
	started        bool
}

func newLease(session *session, localID string) *lease {
	return &lease{session: session, localID: localID, bot: session.bot}
}

// begin records the temporary conversation before the upstream request.
func (l *lease) begin(context.Context) error {
	l.started = true
	return nil
}

// observe claims the upstream conversation id from an SSE_ACK event. It only
// accepts an ack whose local id matches this request.
func (l *lease) observe(event string, data map[string]any) error {
	if event != "SSE_ACK" {
		return nil
	}
	meta, _ := data["ack_client_meta"].(map[string]any)
	if meta == nil {
		return nil
	}
	if got := strings.TrimSpace(asString(meta["local_conversation_id"])); got != "" && got != l.localID {
		return newCodeError(codeInvalidParam, "doubao ack does not match this request")
	}
	id := strings.TrimSpace(asString(meta["conversation_id"]))
	if !isPositiveInt(id) {
		return nil
	}
	if l.conversationID != "" && l.conversationID != id {
		return newCodeError(codeInvalidParam, "doubao returned multiple conversation ids")
	}
	l.conversationID = id
	return nil
}

// finish deletes the temporary conversation. A cleanup error only surfaces when
// the request itself already failed; a delivered reply is never downgraded.
func (l *lease) finish(ctx context.Context, originalErr error) error {
	if !l.started || l.conversationID == "" {
		return nil
	}
	if err := l.delete(ctx); err != nil {
		if originalErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func (l *lease) delete(ctx context.Context) error {
	body := map[string]any{
		"cmd":         imDeleteConvCmd,
		"sequence_id": randomHex(),
		"channel":     2,
		"version":     "1",
		"uplink_body": map[string]any{
			"batch_delete_user_conversation_uplink_body": map[string]any{
				"conversation_id":   []string{l.conversationID},
				"delete_all":        false,
				"conversation_type": conversationType,
			},
		},
	}
	resp, err := l.session.request(ctx, "POST", imDeleteConvPath, body, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var payload struct {
		StatusCode   int             `json:"status_code"`
		StatusDesc   string          `json:"status_desc"`
		DownlinkBody json.RawMessage `json:"downlink_body"`
	}
	raw, err := readAllLimited(resp.Body, 1<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return newCodeError(codeInvalidParam, "doubao conversation delete response is malformed")
	}
	if payload.StatusCode != 0 {
		return newCodeError(codeInvalidParam, "doubao conversation delete failed: "+payload.StatusDesc)
	}
	return nil
}

func randomHex() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte("00000000000000000000000000000000"))
	}
	return hex.EncodeToString(buf)
}

func readAllLimited(r io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, limit))
}
