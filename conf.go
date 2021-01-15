package engine

import (
	log "github.com/pion/ion-log"
	"github.com/pion/webrtc/v3"
)

type ICEConf struct {
	URLs           []string                 `mapstructure:"urls"`
	Username       string                   `mapstructure:"username"`
	Credential     string                   `mapstructure:"credential"`
	CredentialType webrtc.ICECredentialType `mapstructure:"credentialtype"`
}

type WebRTCConf struct {
	ICEPortRange []uint16  `mapstructure:"portrange"`
	ICEServers   []ICEConf `mapstructure:"iceserver"`
	ICELite      bool      `mapstructure:"icelite"`
}

// Config for base AVP
type Config struct {
	Log    log.Config `mapstructure:"log"`
	WebRTC WebRTCConf `mapstructure:"webrtc"`
}
