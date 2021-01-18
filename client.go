package engine

import (
	"encoding/json"
	"path/filepath"

	log "github.com/pion/ion-log"
	"github.com/pion/webrtc/v3"
)

const (
	API_CHANNEL = "ion-sfu"
	PUBLISHER   = 0
	SUBSCRIBER  = 1
)

type Client struct {
	pub    *Transport
	sub    *Transport
	cfg    WebRTCTransportConfig
	signal *Signal

	OnTrack       func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)
	OnDataChannel func(*webrtc.DataChannel)

	producer *WebMProducer
}

func NewClient(addr string, cfg WebRTCTransportConfig) *Client {
	c := &Client{
		signal: NewSignal(addr),
		cfg:    cfg,
	}
	c.pub = NewTransport(PUBLISHER, c.signal, c.cfg)
	c.sub = NewTransport(SUBSCRIBER, c.signal, c.cfg)

	c.signal.OnNegotiate = c.Negotiate
	c.signal.OnTrickle = c.Trickle
	c.signal.OnSetRemoteSDP = c.pub.pc.SetRemoteDescription
	// this will be called when pub add/remove/replace track
	c.pub.pc.OnNegotiationNeeded(c.OnNegotiationNeeded)
	return c
}

func (c *Client) Join(sid string) error {
	log.Infof("sid=%v", sid)
	c.sub.pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		//ReadRTP
		log.Infof("got track %v", track)
		if c.OnTrack != nil {
			c.OnTrack(track, receiver)
		}
	})

	c.sub.pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Infof("got dc %v", dc.Label())
		if dc.Label() == API_CHANNEL {
			// terminate api data channel
			c.sub.api = dc
			return
		}
		log.Infof("got dc %v", dc.Label())
		if c.OnDataChannel != nil {
			c.OnDataChannel(dc)
		}
	})

	offer, err := c.pub.pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	err = c.pub.pc.SetLocalDescription(offer)
	if err != nil {
		return err
	}
	err = c.signal.Join(sid, offer)
	return err
}

func (c *Client) Leave() {
	log.Infof("c=%v", c)
	if c.pub != nil {
		c.pub.pc.Close()
	}
	if c.sub != nil {
		c.sub.pc.Close()
	}

	// err = c.signal.Send(
	// &sfu.SignalRequest{
	// Payload: &sfu.SignalRequest_Leave{
	// Join: &sfu.LeaveRequest{
	// Sid:         sid,
	// },
	// },
	// },
	// )

}

func (c *Client) GetPubStats() webrtc.StatsReport {
	return c.pub.pc.GetStats()
}

func (c *Client) GetSubStats() webrtc.StatsReport {
	return c.sub.pc.GetStats()
}

func (c *Client) Publish(track interface{}) {
	log.Infof("c=%v", c)
	// c.pub.pc.AddTrack()
}

func (c *Client) UnPublish(track interface{}) {
	log.Infof("c=%v", c)
	// c.pub.pc.RemoveTrack()
}

func (c *Client) Close() {
	log.Infof("c=%v", c)
	if c.pub != nil {
		c.pub.pc.Close()
	}
	if c.sub != nil {
		c.sub.pc.Close()
	}
}

// CreateDataChannel create a custom datachannel
func (c *Client) CreateDataChannel(label string) (*webrtc.DataChannel, error) {
	log.Infof("c=%v", c)
	return c.pub.pc.CreateDataChannel(label, &webrtc.DataChannelInit{})
}

func (c *Client) Trickle(candidate webrtc.ICECandidateInit, target int) {
	log.Infof("candidate=%v target=%v", candidate, target)
	var t *Transport
	if target == SUBSCRIBER {
		t = c.sub
	} else {
		t = c.pub
	}

	if t.pc.CurrentRemoteDescription() == nil {
		t.candidates = append(t.candidates, candidate)
	} else {
		err := t.pc.AddICECandidate(candidate)
		if err != nil {
			log.Errorf("err=%v", err)
		}
	}

}

func (c *Client) Negotiate(sdp webrtc.SessionDescription) error {
	log.Infof("sdp=%v", sdp)
	if len(c.sub.candidates) > 0 {
		for _, candidate := range c.sub.candidates {
			_ = c.sub.pc.AddICECandidate(candidate)
		}
		c.sub.candidates = []webrtc.ICECandidateInit{}
	}

	answer, err := c.sub.pc.CreateAnswer(nil)
	if err != nil {
		return err
	}

	err = c.sub.pc.SetLocalDescription(answer)
	if err != nil {
		return err
	}

	c.signal.Answer(answer)

	return err
}

// OnNegotiationNeeded will be called when add/remove track
func (c *Client) OnNegotiationNeeded() {
	log.Infof("OnNegotiationNeeded!!")
	offer, err := c.pub.pc.CreateOffer(nil)
	if err != nil {
		log.Errorf("err=%v", err)
	}
	_ = c.pub.pc.SetLocalDescription(offer)

	c.signal.Offer(offer)
}

type Call struct {
	StreamID string `json:"streamId"`
	Video    string `json:"video"`
	Audio    bool   `json:"audio"`
}

// SelectRemote select remote video/audio
func (c *Client) SelectRemote(streamId, video string, audio bool) {
	log.Infof("streamId=%v video=%v audio=%v", streamId, video, audio)
	call := Call{
		StreamID: streamId,
		Video:    video,
		Audio:    audio,
	}
	marshalled, err := json.Marshal(call)
	if err != nil {
		return
	}
	err = c.sub.api.Send(marshalled)
	if err != nil {
		log.Errorf("err=%v", err)
	}
}

// AddProducer a webm producer
func (c *Client) AddProducer(file string) error {
	ext := filepath.Ext(file)
	switch ext {
	case ".webm":
		c.producer = NewWebMProducer(file, 0)
	default:
		return errInvalidFile
	}
	_, err := c.producer.AddTrack(c.pub.pc, "video")
	if err != nil {
		log.Infof("err=%v", err)
		return err
	}
	_, err = c.producer.AddTrack(c.pub.pc, "audio")
	if err != nil {
		log.Infof("err=%v", err)
		return err
	}
	c.producer.Start()
	return nil
}
