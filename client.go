package engine

import (
	"encoding/json"
	"io"
	"path/filepath"
	"sync"
	"time"

	log "github.com/pion/ion-log"
	"github.com/pion/webrtc/v3"
)

const (
	API_CHANNEL = "ion-sfu"
	PUBLISHER   = 0
	SUBSCRIBER  = 1
)

//Call dc api
type Call struct {
	StreamID string `json:"streamId"`
	Video    string `json:"video"`
	Audio    bool   `json:"audio"`
}

// Client a sdk client
type Client struct {
	pub    *Transport
	sub    *Transport
	cfg    WebRTCTransportConfig
	signal *Signal

	//export to user
	OnTrack       func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)
	OnDataChannel func(*webrtc.DataChannel)

	producer *WebMProducer
	recvByte int
	notify   chan struct{}

	//cache remote sid for subscribe/unsubscribe
	streamLock     sync.RWMutex
	remoteStreamId map[string]string

	//cache datachannel api operation before dc.OnOpen
	apiQueue []Call
}

// NewClient create a sdk client
func NewClient(addr string, cfg WebRTCTransportConfig) *Client {
	c := &Client{
		signal:         NewSignal(addr),
		cfg:            cfg,
		notify:         make(chan struct{}),
		remoteStreamId: make(map[string]string),
	}
	c.pub = NewTransport(PUBLISHER, c.signal, c.cfg)
	c.sub = NewTransport(SUBSCRIBER, c.signal, c.cfg)

	c.signal.OnNegotiate = c.Negotiate
	c.signal.OnTrickle = c.Trickle
	c.signal.OnSetRemoteSDP = c.SetRemoteSDP

	// this will be called when pub add/remove/replace track, but pion never triger, why?
	// c.pub.pc.OnNegotiationNeeded(c.OnNegotiationNeeded)
	return c
}

// SetRemoteSDP pub SetRemoteDescription and send cadidate to sfu
func (c *Client) SetRemoteSDP(sdp webrtc.SessionDescription) error {
	err := c.pub.pc.SetRemoteDescription(sdp)
	if err != nil {
		log.Errorf("err=%v", err)
		return err
	}

	// it's safe to add cand now after SetRemoteDescription
	if len(c.pub.RecvCandidates) > 0 {
		for _, candidate := range c.pub.RecvCandidates {
			log.Infof("c.pub.pc.AddICECandidate candidate=%v", candidate)
			err = c.pub.pc.AddICECandidate(candidate)
			if err != nil {
				log.Errorf("c.pub.pc.AddICECandidate err=%v", err)
			}
		}
		c.pub.RecvCandidates = []webrtc.ICECandidateInit{}
	}

	// it's safe to send cand now after join ok
	if len(c.pub.SendCandidates) > 0 {
		for _, cand := range c.pub.SendCandidates {
			log.Infof("sending c.pub.SendCandidates cand=%v", cand)
			c.signal.Trickle(cand, PUBLISHER)
		}
		c.pub.SendCandidates = []*webrtc.ICECandidate{}
	}
	return nil
}

// Join client join a session
func (c *Client) Join(sid string) error {
	log.Infof("[Client.Join] sid=%v", sid)
	c.sub.pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Infof("[c.sub.pc.OnTrack] got track streamId=%v kind=%v ssrc=%v ", track.StreamID(), track.Kind(), track.SSRC())
		c.streamLock.Lock()
		c.remoteStreamId[track.StreamID()] = track.StreamID()
		log.Infof("c.remoteStreamId=%+v", c.remoteStreamId)
		c.streamLock.Unlock()
		for {
			select {
			case <-c.notify:
				return
			default:
				pkt, _, err := track.ReadRTP()
				if err != nil {
					if err == io.EOF {
						log.Errorf("track.ReadRTP err=%v", err)
						return
					}
					log.Errorf("Error reading track rtp %s", err)
					continue
				}
				c.recvByte += len(pkt.Raw)
			}
		}
		// user define
		// if c.OnTrack != nil {
		// c.OnTrack(track, receiver)
		// }
	})

	c.sub.pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Infof("[c.sub.pc.OnDataChannel] got dc %v", dc.Label())
		if dc.Label() == API_CHANNEL {
			log.Infof("got dc %v", dc.Label())
			c.sub.api = dc
			// send cmd after open
			c.sub.api.OnOpen(func() {
				if len(c.apiQueue) > 0 {
					for _, cmd := range c.apiQueue {
						log.Infof("c.sub.api.OnOpen send cmd=%v", cmd)
						marshalled, err := json.Marshal(cmd)
						if err != nil {
							continue
						}
						err = c.sub.api.Send(marshalled)
						if err != nil {
							log.Errorf("err=%v", err)
						}
						time.Sleep(time.Millisecond * 10)
					}
					c.apiQueue = []Call{}
				}
			})
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

// GetPubStats get pub stats
func (c *Client) GetPubStats() webrtc.StatsReport {
	return c.pub.pc.GetStats()
}

// GetSubStats get sub stats
func (c *Client) GetSubStats() webrtc.StatsReport {
	return c.sub.pc.GetStats()
}

// Publish a local track
func (c *Client) Publish(track webrtc.TrackLocal) (*webrtc.RTPTransceiver, error) {
	t, err := c.pub.pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendonly,
	})
	c.OnNegotiationNeeded()
	return t, err
}

// UnPublish a local track by Transceiver
func (c *Client) UnPublish(t *webrtc.RTPTransceiver) error {
	err := c.pub.pc.RemoveTrack(t.Sender())
	c.OnNegotiationNeeded()
	return err
}

// Close client close
func (c *Client) Close() {
	log.Infof("c=%v", c)
	close(c.notify)
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

// Trickle receive candidate from sfu and add to pc
func (c *Client) Trickle(candidate webrtc.ICECandidateInit, target int) {
	log.Infof("candidate=%v target=%v", candidate, target)
	var t *Transport
	if target == SUBSCRIBER {
		t = c.sub
	} else {
		t = c.pub
	}

	if t.pc.CurrentRemoteDescription() == nil {
		t.RecvCandidates = append(t.RecvCandidates, candidate)
	} else {
		err := t.pc.AddICECandidate(candidate)
		if err != nil {
			log.Errorf("err=%v", err)
		}
	}

}

// Negotiate sub negotiate
func (c *Client) Negotiate(sdp webrtc.SessionDescription) error {
	log.Infof("Negotiate sdp=%v", sdp)
	// 1.sub set remote sdp
	err := c.sub.pc.SetRemoteDescription(sdp)
	if err != nil {
		log.Errorf("Negotiate c.sub.pc.SetRemoteDescription err=%v", err)
		return err
	}

	// 2. safe to send candiate to sfu after join ok
	if len(c.sub.SendCandidates) > 0 {
		for _, cand := range c.sub.SendCandidates {
			log.Infof("send sub.SendCandidates c.signal.Trickle cand=%v", cand)
			c.signal.Trickle(cand, SUBSCRIBER)
		}
		c.sub.SendCandidates = []*webrtc.ICECandidate{}
	}

	// 3. safe to add candidate after SetRemoteDescription
	if len(c.sub.RecvCandidates) > 0 {
		for _, candidate := range c.sub.RecvCandidates {
			log.Infof("Negotiate c.sub.pc.AddICECandidate candidate=%v", candidate)
			_ = c.sub.pc.AddICECandidate(candidate)
		}
		c.sub.RecvCandidates = []webrtc.ICECandidateInit{}
	}

	// 4. create answer after add ice candidate
	answer, err := c.sub.pc.CreateAnswer(nil)
	if err != nil {
		log.Errorf("err=%v", err)
		return err
	}

	// 5. set local sdp(answer)
	err = c.sub.pc.SetLocalDescription(answer)
	if err != nil {
		log.Errorf("err=%v", err)
		return err
	}

	// 6. send answer to sfu
	c.signal.Answer(answer)

	return err
}

// OnNegotiationNeeded will be called when add/remove track, but never trigger, call by hand
func (c *Client) OnNegotiationNeeded() {
	// 1. pub create offer
	offer, err := c.pub.pc.CreateOffer(nil)
	if err != nil {
		log.Errorf("err=%v", err)
	}

	// 2. pub set local sdp(offer)
	err = c.pub.pc.SetLocalDescription(offer)
	if err != nil {
		log.Errorf("err=%v", err)
	}

	log.Infof("OnNegotiationNeeded!! c.pub.pc.CreateOffer and send offer=%v", offer)
	//3. send offer to sfu
	c.signal.Offer(offer)
}

// selectRemote select remote video/audio
func (c *Client) selectRemote(streamId, video string, audio bool) error {
	log.Infof("streamId=%v video=%v audio=%v", streamId, video, audio)
	call := Call{
		StreamID: streamId,
		Video:    video,
		Audio:    audio,
	}

	// cache cmd when dc not ready
	if c.sub.api == nil || c.sub.api.ReadyState() != webrtc.DataChannelStateOpen {
		log.Infof("append to c.apiQueue call=%v", call)
		c.apiQueue = append(c.apiQueue, call)
		return nil
	}

	// send cached cmd
	if len(c.apiQueue) > 0 {
		for _, cmd := range c.apiQueue {
			log.Infof("c.sub.api.Send cmd=%v", cmd)
			marshalled, err := json.Marshal(cmd)
			if err != nil {
				continue
			}
			err = c.sub.api.Send(marshalled)
			if err != nil {
				log.Errorf("err=%v", err)
			}
			time.Sleep(time.Millisecond * 10)
		}
		c.apiQueue = []Call{}
	}

	// send this cmd
	log.Infof("c.sub.api.Send call=%v", call)
	marshalled, err := json.Marshal(call)
	if err != nil {
		return err
	}
	err = c.sub.api.Send(marshalled)
	if err != nil {
		log.Errorf("err=%v", err)
	}
	return err
}

// UnSubscribeAll unsubscribe all stream
func (c *Client) UnSubscribeAll() {
	c.streamLock.RLock()
	m := c.remoteStreamId
	c.streamLock.RUnlock()
	for streamId := range m {
		log.Infof("UnSubscribe remote streamid=%v", streamId)
		c.selectRemote(streamId, "none", false)
	}
}

// SubscribeAll subscribe all stream with the same video/audio param
func (c *Client) SubscribeAll(video string, audio bool) {
	c.streamLock.RLock()
	m := c.remoteStreamId
	c.streamLock.RUnlock()
	for streamId := range m {
		log.Infof("Subscribe remote streamid=%v", streamId)
		c.selectRemote(streamId, video, audio)
	}
}

// PublishWebm publish a webm producer
func (c *Client) PublishWebm(file string) error {
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
	//occur by hand
	c.OnNegotiationNeeded()
	return nil
}

func (c *Client) getBandWidth(cycle int) (int, int) {
	var recvBW, sendBW int
	if c.producer != nil {
		sendBW = c.producer.GetSendBandwidth(cycle)
	}

	recvBW = c.recvByte / cycle / 1000
	c.recvByte = 0
	return recvBW, sendBW
}
