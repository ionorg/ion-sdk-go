package client

import (
	"github.com/pion/interceptor"
	logr "github.com/pion/ion-sfu/pkg/logger"
	"github.com/pion/webrtc/v3"
)

var log = logr.New().WithName("client")

const (
	rolePublish   int = 0
	roleSubscribe int = 1
)

type transport struct {
	role       int
	api        *webrtc.DataChannel
	pc         *webrtc.PeerConnection
	signal     Signal
	candidates []*webrtc.ICECandidateInit
}

func newTransport(role int, signal Signal, cfg *webrtc.Configuration, extraInterceptors []interceptor.Interceptor) (*transport, error) {
	me, _ := getProducerMediaEngine()
	se := webrtc.SettingEngine{}

	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(me, i); err != nil {
		return nil, err
	}
	for _, itc := range extraInterceptors {
		i.Add(itc)
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(se), webrtc.WithInterceptorRegistry(i))
	pc, err := api.NewPeerConnection(*cfg)
	if err != nil {
		return nil, err
	}

	t := &transport{
		role:       role,
		signal:     signal,
		candidates: []*webrtc.ICECandidateInit{},
		pc:         pc,
	}

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			trickle := candidate.ToJSON()
			signal.Trickle(role, &trickle)
		}
	})

	pc.OnDataChannel(func(channel *webrtc.DataChannel) {
		log.Info("transport got datachannel", "label", channel.Label())
		//todo handle api / remoteStream
		t.api = channel
	})

	if role == rolePublish {
		t.api, _ = pc.CreateDataChannel("ion-sfu", nil)
	}

	return t, nil
}

// Client for ion-cluster
type Client struct {
	signal Signal

	pub *transport
	sub *transport

	OnTrack func(*webrtc.TrackRemote, *webrtc.RTPReceiver, *webrtc.PeerConnection)
}

//NewClient returns a new jsonrpc2 client that manages a pub and sub peerConnection
func NewClient(signal Signal, cfg *webrtc.Configuration, pubInterceptors []interceptor.Interceptor) (*Client, error) {
	pub, err := newTransport(rolePublish, signal, cfg, pubInterceptors)
	if err != nil {
		return nil, err
	}
	sub, err := newTransport(roleSubscribe, signal, cfg, []interceptor.Interceptor{})
	if err != nil {
		return nil, err
	}

	return &Client{
		signal: signal,
		pub:    pub,
		sub:    sub,
	}, nil
}

//Join a session
func (c *Client) Join(sid string) error {
	c.signal.OnNegotiate(c.signalOnNegotiate)
	c.signal.OnTrickle(c.signalOnTrickle)

	c.sub.pc.OnTrack(func(track *webrtc.TrackRemote, recv *webrtc.RTPReceiver) {
		log.Info("client sub got remote track", "streamID", track.Msid(), "trackID", track.ID())
		if c.OnTrack != nil {
			c.OnTrack(track, recv, c.sub.pc)
		}
	})

	// Setup Pub PC
	offer, err := c.pub.pc.CreateOffer(nil)
	if err != nil {
		log.Error(err, "client join could not create pub offer")
		return err
	}
	if err := c.pub.pc.SetLocalDescription(offer); err != nil {
		log.Error(err, "client join pub couldn't SetLocalDescription")
		return err
	}

	answer, err := c.signal.Join(sid, &offer)
	if err != nil {
		log.Error(err, "client join signal error")
		return err
	}
	if err := c.pub.pc.SetRemoteDescription(*answer); err != nil {
		log.Error(err, "client join pub couldn't SetRemoteDescription")
		return err
	}

	for _, candidate := range c.pub.candidates {
		c.pub.pc.AddICECandidate(*candidate)
	}
	c.pub.pc.OnNegotiationNeeded(c.pubNegotiationNeeded)

	return nil
}

// Publish takes a producer and publishes its data to the peer connection
func (c *Client) Publish(p Producer) error {
	videoSender, err := c.pub.pc.AddTrack(p.VideoTrack())
	if err != nil {
		return err
	}
	audioSender, err := c.pub.pc.AddTrack(p.AudioTrack())
	if err != nil {
		return err

	}
	defer c.pubNegotiationNeeded()

	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := videoSender.Read(rtcpBuf); rtcpErr != nil {
				log.Error(err, "videoSender rtcp error")
				return
			}
		}
	}()

	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := audioSender.Read(rtcpBuf); rtcpErr != nil {
				log.Error(err, "audioSender rtcp error")
				return
			}
		}
	}()

	go p.Start()
	return nil
}

// Pub PC re-negotiation
func (c *Client) pubNegotiationNeeded() {
	log.Info("client pubOnNegotiationNeeded")
	offer, err := c.pub.pc.CreateOffer(nil)
	if err != nil {
		log.Error(err, "pub could not create pub offer")
		return
	}
	if err := c.pub.pc.SetLocalDescription(offer); err != nil {
		log.Error(err, "pub couldn't SetLocalDescription")
		return
	}

	answer, err := c.signal.Offer(&offer)
	if err != nil {
		log.Error(err, "pub signal error")
		return
	}
	if err := c.pub.pc.SetRemoteDescription(*answer); err != nil {
		log.Error(err, "pub couldn't SetRemoteDescription")
		return
	}

	log.Info("client negotiated")
}

// CreateDatachannel to publish
func (c *Client) CreateDatachannel(label string) (*webrtc.DataChannel, error) {
	return c.pub.pc.CreateDataChannel(label, nil)
}

// signalOnNegotiate is triggered from server for the sub pc
func (c *Client) signalOnNegotiate(desc *webrtc.SessionDescription) {
	if err := c.sub.pc.SetRemoteDescription(*desc); err != nil {
		log.Error(err, "sub couldn't SetRemoteDescription")
		return
	}

	for _, candidate := range c.sub.candidates {
		c.sub.pc.AddICECandidate(*candidate)
	}
	c.sub.candidates = []*webrtc.ICECandidateInit{}

	answer, err := c.sub.pc.CreateAnswer(nil)
	if err != nil {
		log.Error(err, "sub couldn't create answer")
		return
	}
	if err := c.sub.pc.SetLocalDescription(answer); err != nil {
		log.Error(err, "sub couldn't setLocalDescription")
		return
	}

	c.signal.Answer(&answer)
}

// signalOnNegotiate is triggered from server for the sub pc
func (c *Client) signalOnTrickle(role int, candidate *webrtc.ICECandidateInit) {
	var target *transport
	switch role {
	case rolePublish:
		target = c.pub
	case roleSubscribe:
		target = c.sub
	}

	if target.pc.RemoteDescription() != nil {
		target.pc.AddICECandidate(*candidate)
	} else {
		target.candidates = append(target.candidates, candidate)
	}

}
