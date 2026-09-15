package devin

const (
	CredentialFormat = "devin-session-v1"

	AppBase    = "https://app.devin.ai"
	APIBase    = "https://api.devin.ai"
	ServerBase = "https://server.codeium.com"

	PathGetChatMessage     = "/exa.api_server_pb.ApiServerService/GetChatMessage"
	PathGetUserStatus      = "/exa.seat_management_pb.SeatManagementService/GetUserStatus"
	PathGetCliModelConfigs = "/exa.api_server_pb.ApiServerService/GetCliModelConfigs"
	PathAuthContinue       = "/auth/cli/continue"
	PathAuthToken          = "/auth/cli/token"
	PathSelf               = "/v3/self"

	TokenPrefix = "devin-session-token$"

	ClientProductLabel = "devin-cli"
	ClientName         = "chisel"
	ClientVersion      = "3000.10.21"

	FingerprintHexLen = 732
	DefaultMaxTokens  = 128000
	DefaultModelUID   = "swe-2-high"

	ContentTypeConnectProto = "application/connect+proto"
	ContentTypeProto        = "application/proto"
	ConnectProtocolVersion  = "1"

	QuotaUnit = "percent"

	ConnectFlagData       byte = 0x00
	ConnectFlagCompressed byte = 0x01
	ConnectFlagEndStream  byte = 0x02

	maxConnectFrameSize      = 16 * 1024 * 1024
	maxDecompressedFrameSize = 64 * 1024 * 1024
)
