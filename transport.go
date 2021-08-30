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
	role           Target
	SendCandidates []*webrtc.ICECandidate
	RecvCandidates []webrtc.ICECandidateInit
}

// NewTransport create a transport
func NewTransport(role Target, signal *Signal) *Transport {
	t := &Transport{
		role:   role,
		signal: signal,
	}

	var err error
	var api *webrtc.API
	var me *webrtc.MediaEngine
	DefaultConfig.WebRTC.Setting.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	if role == Target_PUBLISHER {
		me, err = getPublisherMediaEngine(DefaultConfig.WebRTC.VideoMime)
	} else {
		me, err = getSubscriberMediaEngine()
	}

	if err != nil {
		log.Errorf("getPublisherMediaEngine error: %v", err)
		return nil
	}

	api = webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(DefaultConfig.WebRTC.Setting))
	t.pc, err = api.NewPeerConnection(DefaultConfig.WebRTC.Configuration)

	if err != nil {
		log.Errorf("NewPeerConnection error: %v", err)
		return nil
	}

	if role == Target_PUBLISHER {
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
				t.signal.trickle(cand, role)
			}
			t.SendCandidates = []*webrtc.ICECandidate{}
			t.signal.trickle(c, role)
		}
	})
	return t
}

func (t *Transport) GetPeerConnection() *webrtc.PeerConnection {
	return t.pc
}
