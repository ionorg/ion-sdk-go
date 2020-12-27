package engine

import log "github.com/pion/ion-log"

type iceconf struct {
	URLs       []string `mapstructure:"urls"`
	Username   string   `mapstructure:"username"`
	Credential string   `mapstructure:"credential"`
}

type webrtcconf struct {
	ICEPortRange []uint16  `mapstructure:"portrange"`
	ICEServers   []iceconf `mapstructure:"iceserver"`
}

// Config for base AVP
type Config struct {
	Log    log.Config `mapstructure:"log"`
	WebRTC webrtcconf `mapstructure:"webrtc"`
}
