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
