package engine

func NewJoinConfig() *JoinConfig {
	m := make(JoinConfig)
	return &m
}

type JoinConfig map[string]string

func (j JoinConfig) SetNoPublish() *JoinConfig {
	j["NoPublish"] = "true"
	return &j
}

func (j JoinConfig) SetNoSubscribe() *JoinConfig {
	j["NoSubscribe"] = "true"
	return &j
}

func (j JoinConfig) SetNoAutoSubscribe() *JoinConfig {
	j["NoAutoSubscribe"] = "true"
	return &j
}

func SetRelay(j JoinConfig) *JoinConfig {
	j["Relay"] = "true"
	return &j
}
