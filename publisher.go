package engine

import (
	"path/filepath"

	log "github.com/pion/ion-log"

	"github.com/pion/webrtc/v3"
)

type Publisher struct {
	pc         *webrtc.PeerConnection
	candidates []webrtc.ICECandidateInit
	producer   *WebMProducer
}

// NewPublisher creates a new Publisher
func NewPublisher(cfg WebRTCTransportConfig) (*Publisher, error) {
	api := webrtc.NewAPI(webrtc.WithSettingEngine(cfg.Setting))
	pc, err := api.NewPeerConnection(cfg.Configuration)

	if err != nil {
		log.Errorf("NewPeer error: %v", err)
		return nil, errPeerConnectionInitFailed
	}

	_, err = pc.CreateDataChannel("ion-sfu", &webrtc.DataChannelInit{})

	if err != nil {
		log.Errorf("error creating data channel: %v", err)
		return nil, errPeerConnectionInitFailed
	}

	return &Publisher{
		pc: pc,
	}, nil
}

func (p *Publisher) AddProducer(file string) error {
	ext := filepath.Ext(file)
	switch ext {
	case ".webm":
		p.producer = NewWebMProducer(file, 0)
	default:
		return errInvalidFile
	}
	_, err := p.producer.AddTrack(p.pc, "video")
	if err != nil {
		log.Infof("err=%v", err)
		return err
	}
	_, err = p.producer.AddTrack(p.pc, "audio")
	if err != nil {
		log.Infof("err=%v", err)
		return err
	}
	p.producer.Start()
	return nil
}

func (p *Publisher) CreateOffer() (webrtc.SessionDescription, error) {
	log.Infof("CreateOffer")
	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}

	log.Infof("p.pc.SetLocalDescription")
	err = p.pc.SetLocalDescription(offer)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	log.Infof("CreateOffer ok!!")

	return offer, nil
}

// OnICECandidate handler
func (p *Publisher) OnICECandidate(f func(c *webrtc.ICECandidate)) {
	p.pc.OnICECandidate(f)
}

func (p *Publisher) OnNegotiationNeeded(f func()) {
	p.pc.OnNegotiationNeeded(f)
}

// AddICECandidate to peer connection
func (p *Publisher) AddICECandidate(candidate webrtc.ICECandidateInit) error {
	if p.pc.RemoteDescription() != nil {
		return p.pc.AddICECandidate(candidate)
	}
	p.candidates = append(p.candidates, candidate)
	return nil
}

// SetRemoteDescription sets the SessionDescription of the remote peer
func (p *Publisher) SetRemoteDescription(desc webrtc.SessionDescription) error {
	err := p.pc.SetRemoteDescription(desc)
	if err != nil {
		log.Errorf("SetRemoteDescription error: %v", err)
		return err
	}

	return nil
}

// Close peer
func (p *Publisher) Close() error {
	return p.pc.Close()
}
