package engine

import (
	"github.com/pion/webrtc/v3"
)

// Config ..
type Config struct {
	// WebRTC WebRTCConf `mapstructure:"webrtc"`
	LogLevel string                `mapstructure:"log"`
	WebRTC   WebRTCTransportConfig `mapstructure:"webrtc"`
}

// WebRTCTransportConfig represents configuration options
type WebRTCTransportConfig struct {
	VideoMime     string
	Configuration webrtc.Configuration
	Setting       webrtc.SettingEngine
}

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

func SetRelay(j JoinConfig) *JoinConfig {
	j["Relay"] = "true"
	return &j
}
