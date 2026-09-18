package runtime

var _ ProcessStarter = (*ExecStarter)(nil)
var _ ProxyConfigurableStarter = (*ExecStarter)(nil)
var _ APIKeyConfigurableStarter = (*ExecStarter)(nil)
