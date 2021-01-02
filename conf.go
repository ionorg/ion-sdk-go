package engine

import log "github.com/pion/ion-log"

type ICEConf struct {
	URLs       []string `mapstructure:"urls"`
	Username   string   `mapstructure:"username"`
	Credential string   `mapstructure:"credential"`
}

type WebRTCConf struct {
	ICEPortRange []uint16  `mapstructure:"portrange"`
	ICEServers   []ICEConf `mapstructure:"iceserver"`
}

// Config for base AVP
type Config struct {
	Log    log.Config `mapstructure:"log"`
	WebRTC WebRTCConf `mapstructure:"webrtc"`
}
