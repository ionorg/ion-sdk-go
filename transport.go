package engine

import (
	"github.com/pion/ice/v2"
	"github.com/pion/webrtc/v3"
)

// Transport is pub/sub transport
type Transport struct {
	api            *webrtc.DataChannel
	signal         *Signal
	pc             *webrtc.PeerConnection
	role           int
	config         WebRTCTransportConfig
	SendCandidates []*webrtc.ICECandidate
	RecvCandidates []webrtc.ICECandidateInit
}

// NewTransport create a transport
func NewTransport(role int, signal *Signal, cfg WebRTCTransportConfig) *Transport {
	t := &Transport{
		role:   role,
		signal: signal,
		config: cfg,
	}

	var err error
	var api *webrtc.API
	var me *webrtc.MediaEngine
	cfg.Setting.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	if role == PUBLISHER {
		me, err = getPublisherMediaEngine(cfg.VideoMime)
	} else {
		me, err = getSubscriberMediaEngine()
	}
	api = webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(cfg.Setting))
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
		//append before join session success
		if t.pc.CurrentRemoteDescription() == nil {
			t.SendCandidates = append(t.SendCandidates, c)
		} else {
			for _, cand := range t.SendCandidates {
				t.signal.Trickle(cand, role)
			}
			t.SendCandidates = []*webrtc.ICECandidate{}
			t.signal.Trickle(c, role)
		}
	})
	return t
}

func (t *Transport) GetPeerConnection() *webrtc.PeerConnection {
	return t.pc
}
