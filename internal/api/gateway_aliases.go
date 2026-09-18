package api

import (
	"io"
	"net/http"

	"github.com/caigee-cmd/cli2api/internal/executor"
	apigateway "github.com/caigee-cmd/cli2api/internal/gateway"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

type streamRelayStats = apigateway.StreamRelayStats
type streamFlushWriter = apigateway.StreamFlushWriter

func relayOpenAIStream(w http.ResponseWriter, body io.Reader) (streamRelayStats, error) {
	return apigateway.RelayOpenAIStream(w, body)
}

func relayAnthropicStream(writer io.Writer, body io.Reader, requestID, model string) (streamRelayStats, error) {
	return apigateway.RelayAnthropicStream(writer, body, requestID, model)
}

func relayResponsesStream(writer io.Writer, body io.Reader, requestID, model string) (streamRelayStats, error) {
	return apigateway.RelayResponsesStream(writer, body, requestID, model)
}

func sseDeltaHasToken(line string) bool {
	return apigateway.SSEDeltaHasToken(line)
}

func isStreamClientDisconnect(err error) bool {
	return apigateway.IsStreamClientDisconnect(err)
}

func classifyAPIError(err error) executor.Classified {
	return apigateway.ClassifyAPIError(err)
}

func writeClassifiedErr(w http.ResponseWriter, err error) {
	apigateway.WriteClassifiedErr(w, err)
}

func buildChatUsage(res executor.ChatResult) map[string]any {
	return apigateway.BuildChatUsage(res)
}

func providerErrorFromClassified(classified executor.Classified) *providers.Error {
	return apigateway.ProviderErrorFromClassified(classified)
}

func parseStreamUsageLine(line string) (streamRelayStats, bool) {
	return apigateway.ParseStreamUsageLine(line)
}

type streamRelayWriteError = apigateway.StreamRelayWriteError
