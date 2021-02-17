package engine

import (
	log "github.com/pion/ion-log"
	"github.com/pion/webrtc/v3"
)

// Config ..
type Config struct {
	Log log.Config `mapstructure:"log"`
	// WebRTC WebRTCConf `mapstructure:"webrtc"`
	WebRTC WebRTCTransportConfig `mapstructure:"webrtc"`
}

// WebRTCTransportConfig represents configuration options
type WebRTCTransportConfig struct {
	VideoMime     string
	Configuration webrtc.Configuration
	Setting       webrtc.SettingEngine
}
