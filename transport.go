package engine

import (
	"encoding/json"

	log "github.com/pion/ion-log"
	"github.com/pion/webrtc/v3"
)

type Transport struct {
	api        *webrtc.DataChannel
	signal     *Signal
	pc         *webrtc.PeerConnection
	role       int
	config     WebRTCTransportConfig
	candidates []webrtc.ICECandidateInit
}

// NewTransport create a transport
func NewTransport(role int, signal *Signal, cfg WebRTCTransportConfig) *Transport {
	var err error
	t := &Transport{
		role:   role,
		signal: signal,
		config: cfg,
	}

	me := &webrtc.MediaEngine{}
	_ = me.RegisterDefaultCodecs()
	api := webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(cfg.Setting))
	t.pc, err = api.NewPeerConnection(cfg.Configuration)

	if err != nil {
		log.Errorf("NewPeerConnection error: %v", err)
		return nil
	}

	if role == PUBLISHER {
		_, err = t.pc.CreateDataChannel(API_CHANNEL, &webrtc.DataChannelInit{})

		if err != nil {
			log.Errorf("error creating data channel: %v", err)
			return nil
		}
	}

	t.pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			// Gathering done
			log.Infof("gather candidate done")
			return
		}
		bytes, err := json.Marshal(c.ToJSON())
		if err != nil {
			log.Errorf("OnIceCandidate error %s", err)
		}
		log.Infof("send ice candidate=%v role=%v", string(bytes), role)
		t.signal.Trickle(c, role)
	})
	return t
}
