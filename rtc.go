package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/pion/ion-log"
	"github.com/pion/ion/proto/rtc"
	"github.com/pion/webrtc/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	API_CHANNEL = "ion-sfu"
)

//Call dc api
type Call struct {
	StreamID string `json:"streamId"`
	Video    string `json:"video"`
	Audio    bool   `json:"audio"`
}

type TrackInfo struct {
	Id        string
	Kind      string
	Muted     bool
	Type      MediaType
	StreamId  string
	Label     string
	Subscribe bool
	Layer     string
	Direction string
	Width     uint32
	Height    uint32
	FrameRate uint32
}

type Subscription struct {
	TrackId   string
	Mute      bool
	Subscribe bool
	Layer     string
}

type Target int32

const (
	Target_PUBLISHER  Target = 0
	Target_SUBSCRIBER Target = 1
)

type MediaType int32

const (
	MediaType_MediaUnknown  MediaType = 0
	MediaType_UserMedia     MediaType = 1
	MediaType_ScreenCapture MediaType = 2
	MediaType_Cavans        MediaType = 3
	MediaType_Streaming     MediaType = 4
	MediaType_VoIP          MediaType = 5
)

type TrackEvent_State int32

const (
	TrackEvent_ADD    TrackEvent_State = 0
	TrackEvent_UPDATE TrackEvent_State = 1
	TrackEvent_REMOVE TrackEvent_State = 2
)

// TrackEvent info
type TrackEvent struct {
	State  TrackEvent_State
	Uid    string
	Tracks []*TrackInfo
}

var (
	DefaultConfig = RTCConfig{
		WebRTC: WebRTCTransportConfig{
			Configuration: webrtc.Configuration{
				ICEServers: []webrtc.ICEServer{
					{
						URLs: []string{"stun:stun.stunprotocol.org:3478", "stun:stun.l.google.com:19302"},
					},
				},
			},
		},
	}
)

// WebRTCTransportConfig represents configuration options
type WebRTCTransportConfig struct {
	// if set, only this codec will be registered. leave unset to register all codecs.
	VideoMime     string
	Configuration webrtc.Configuration
	Setting       webrtc.SettingEngine
}

type RTCConfig struct {
	WebRTC WebRTCTransportConfig `mapstructure:"webrtc"`
}

// Signaller sends and receives signalling messages with peers.
// Signaller is derived from rtc.RTC_SignalClient, matching the
// exported API of the GRPC Signal Service.
// Signaller allows alternative signalling implementations
// if the GRPC Signal Service does not fit your use case.
type Signaller interface {
	Send(request *rtc.Request) error
	Recv() (*rtc.Reply, error)
	CloseSend() error
}

// Client a sdk client
type RTC struct {
	Service
	connected bool

	config *RTCConfig

	uid string
	pub *Transport
	sub *Transport

	//export to user
	OnTrack       func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)
	OnDataChannel func(*webrtc.DataChannel)
	OnError       func(error)
	OnTrackEvent  func(event TrackEvent)
	OnSpeaker     func(event []string)

	producer *WebMProducer
	recvByte int
	notify   chan struct{}

	//cache datachannel api operation before dr.OnOpen
	apiQueue []Call

	signaller Signaller

	ctx        context.Context
	cancel     context.CancelFunc
	handleOnce sync.Once
	sync.Mutex
}

func withConfig(config ...RTCConfig) *RTC {
	r := &RTC{
		notify: make(chan struct{}),
	}
	r.ctx, r.cancel = context.WithCancel(context.Background())

	if len(config) > 0 {
		r.config = &config[0]
	}

	return r
}

// NewRTC creates an RTC using the default GRPC signaller
func NewRTC(connector *Connector, config ...RTCConfig) (*RTC, error) {
	r := withConfig(config...)
	signaller, err := connector.Signal(r)
	r.start(signaller)
	return r, err
}

// NewRTCWithSignaller creates an RTC with a specified signaller
func NewRTCWithSignaller(signaller Signaller, config ...RTCConfig) *RTC {
	r := withConfig(config...)
	r.start(signaller)
	return r
}

func (r *RTC) start(signaller Signaller) {
	r.signaller = signaller

	if !r.Connected() {
		r.Connect()
	}
	r.pub = NewTransport(Target_PUBLISHER, r)
	r.sub = NewTransport(Target_PUBLISHER, r)
}

// Join client join a session
func (r *RTC) Join(sid, uid string, config ...*JoinConfig) error {
	log.Infof("[C=>S] sid=%v uid=%v", sid, uid)
	if uid == "" {
		uid = RandomKey(6)
	}
	r.uid = uid
	r.sub.pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Infof("[S=>C] got track streamId=%v kind=%v ssrc=%v ", track.StreamID(), track.Kind(), track.SSRC())

		// user define
		if r.OnTrack != nil {
			r.OnTrack(track, receiver)
		} else {
			//for read and calc
			b := make([]byte, 1500)
			for {
				select {
				case <-r.notify:
					return
				default:
					n, _, err := track.Read(b)
					if err != nil {
						if err == io.EOF {
							log.Errorf("id=%v track.ReadRTP err=%v", r.uid, err)
							return
						}
						log.Errorf("id=%v Error reading track rtp %s", r.uid, err)
						continue
					}
					r.recvByte += n
				}
			}
		}
	})

	r.sub.pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Debugf("[S=>C] id=%v [r.sub.pc.OnDataChannel] got dc %v", r.uid, dc.Label())
		if dc.Label() == API_CHANNEL {
			log.Debugf("%v got dc %v", r.uid, dc.Label())
			r.sub.api = dc
			// send cmd after open
			r.sub.api.OnOpen(func() {
				if len(r.apiQueue) > 0 {
					for _, cmd := range r.apiQueue {
						log.Debugf("%v r.sub.api.OnOpen send cmd=%v", r.uid, cmd)
						marshalled, err := json.Marshal(cmd)
						if err != nil {
							continue
						}
						err = r.sub.api.Send(marshalled)
						if err != nil {
							log.Errorf("id=%v err=%v", r.uid, err)
						}
						time.Sleep(time.Millisecond * 10)
					}
					r.apiQueue = []Call{}
				}
			})
			return
		}
		log.Debugf("%v got dc %v", r.uid, dc.Label())
		if r.OnDataChannel != nil {
			r.OnDataChannel(dc)
		}
	})

	r.sub.pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		if state >= webrtc.ICEConnectionStateDisconnected {
			log.Infof("ICEConnectionStateDisconnected %v", state)

		}
	})

	offer, err := r.pub.pc.CreateOffer(nil)
	if err != nil {
		return err
	}

	err = r.pub.pc.SetLocalDescription(offer)
	if err != nil {
		return err
	}

	if len(config) > 0 {
		err = r.SendJoin(sid, r.uid, offer, *config[0])
	} else {
		err = r.SendJoin(sid, r.uid, offer, nil)
	}

	if err != nil {
		return err
	}

	return err
}

// GetPubStats get pub stats
func (r *RTC) GetPubStats() webrtc.StatsReport {
	return r.pub.pc.GetStats()
}

// GetSubStats get sub stats
func (r *RTC) GetSubStats() webrtc.StatsReport {
	return r.sub.pc.GetStats()
}

func (r *RTC) GetPubTransport() *Transport {
	return r.pub
}

func (r *RTC) GetSubTransport() *Transport {
	return r.sub
}

// Publish local tracks
func (r *RTC) Publish(tracks ...webrtc.TrackLocal) ([]*webrtc.RTPSender, error) {
	var rtpSenders []*webrtc.RTPSender
	for _, t := range tracks {
		if rtpSender, err := r.pub.GetPeerConnection().AddTrack(t); err != nil {
			log.Errorf("AddTrack error: %v", err)
			return rtpSenders, err
		} else {
			rtpSenders = append(rtpSenders, rtpSender)
		}

	}
	r.onNegotiationNeeded()
	return rtpSenders, nil
}

// UnPublish local tracks by transceivers
func (r *RTC) UnPublish(senders ...*webrtc.RTPSender) error {
	for _, s := range senders {
		if err := r.pub.pc.RemoveTrack(s); err != nil {
			return err
		}
	}
	r.onNegotiationNeeded()
	return nil
}

// CreateDataChannel create a custom datachannel
func (r *RTC) CreateDataChannel(label string) (*webrtc.DataChannel, error) {
	log.Debugf("id=%v CreateDataChannel %v", r.uid, label)
	return r.pub.pc.CreateDataChannel(label, &webrtc.DataChannelInit{})
}

// trickle receive candidate from sfu and add to pc
func (r *RTC) trickle(candidate webrtc.ICECandidateInit, target Target) {
	log.Debugf("[S=>C] id=%v candidate=%v target=%v", r.uid, candidate, target)
	var t *Transport
	if target == Target_SUBSCRIBER {
		t = r.sub
	} else {
		t = r.pub
	}

	if t.pc.CurrentRemoteDescription() == nil {
		t.RecvCandidates = append(t.RecvCandidates, candidate)
	} else {
		err := t.pc.AddICECandidate(candidate)
		if err != nil {
			log.Errorf("id=%v err=%v", r.uid, err)
		}
	}

}

// negotiate sub negotiate
func (r *RTC) negotiate(sdp webrtc.SessionDescription) error {
	log.Debugf("[S=>C] id=%v Negotiate sdp=%v", r.uid, sdp)
	// 1.sub set remote sdp
	err := r.sub.pc.SetRemoteDescription(sdp)
	if err != nil {
		log.Errorf("id=%v Negotiate r.sub.pc.SetRemoteDescription err=%v", r.uid, err)
		return err
	}

	// 2. safe to send candiate to sfu after join ok
	if len(r.sub.SendCandidates) > 0 {
		for _, cand := range r.sub.SendCandidates {
			log.Debugf("[C=>S] id=%v send sub.SendCandidates r.uid, r.rtc.trickle cand=%v", r.uid, cand)
			r.SendTrickle(cand, Target_SUBSCRIBER)
		}
		r.sub.SendCandidates = []*webrtc.ICECandidate{}
	}

	// 3. safe to add candidate after SetRemoteDescription
	if len(r.sub.RecvCandidates) > 0 {
		for _, candidate := range r.sub.RecvCandidates {
			log.Debugf("id=%v r.sub.pc.AddICECandidate candidate=%v", r.uid, candidate)
			_ = r.sub.pc.AddICECandidate(candidate)
		}
		r.sub.RecvCandidates = []webrtc.ICECandidateInit{}
	}

	// 4. create answer after add ice candidate
	answer, err := r.sub.pc.CreateAnswer(nil)
	if err != nil {
		log.Errorf("id=%v err=%v", r.uid, err)
		return err
	}

	// 5. set local sdp(answer)
	err = r.sub.pc.SetLocalDescription(answer)
	if err != nil {
		log.Errorf("id=%v err=%v", r.uid, err)
		return err
	}

	// 6. send answer to sfu
	err = r.SendAnswer(answer)
	if err != nil {
		log.Errorf("id=%v err=%v", r.uid, err)
		return err
	}
	return err
}

// onNegotiationNeeded will be called when add/remove track, but never trigger, call by hand
func (r *RTC) onNegotiationNeeded() {
	// 1. pub create offer
	offer, err := r.pub.pc.CreateOffer(nil)
	if err != nil {
		log.Errorf("id=%v err=%v", r.uid, err)
	}

	// 2. pub set local sdp(offer)
	err = r.pub.pc.SetLocalDescription(offer)
	if err != nil {
		log.Errorf("id=%v err=%v", r.uid, err)
	}

	//3. send offer to sfu
	err = r.SendOffer(offer)
	if err != nil {
		log.Errorf("id=%v err=%v", r.uid, err)
	}
}

// selectRemote select remote video/audio
func (r *RTC) selectRemote(streamId, video string, audio bool) error {
	log.Debugf("id=%v streamId=%v video=%v audio=%v", r.uid, streamId, video, audio)
	call := Call{
		StreamID: streamId,
		Video:    video,
		Audio:    audio,
	}

	// cache cmd when dc not ready
	if r.sub.api == nil || r.sub.api.ReadyState() != webrtc.DataChannelStateOpen {
		log.Debugf("id=%v append to r.apiQueue call=%v", r.uid, call)
		r.apiQueue = append(r.apiQueue, call)
		return nil
	}

	// send cached cmd
	if len(r.apiQueue) > 0 {
		for _, cmd := range r.apiQueue {
			log.Debugf("[C=>S] id=%v r.sub.api.Send cmd=%v", r.uid, cmd)
			marshalled, err := json.Marshal(cmd)
			if err != nil {
				continue
			}
			err = r.sub.api.Send(marshalled)
			if err != nil {
				log.Errorf("error: %v", err)
			}
			time.Sleep(time.Millisecond * 10)
		}
		r.apiQueue = []Call{}
	}

	// send this cmd
	log.Debugf("[C=>S] id=%v r.sub.api.Send call=%v", r.uid, call)
	marshalled, err := json.Marshal(call)
	if err != nil {
		return err
	}
	err = r.sub.api.Send(marshalled)
	if err != nil {
		log.Errorf("id=%v err=%v", r.uid, err)
	}
	return err
}

// PublishWebm publish a webm producer
func (r *RTC) PublishFile(file string, video, audio bool) error {
	if !FileExist(file) {
		return os.ErrNotExist
	}
	ext := filepath.Ext(file)
	switch ext {
	case ".webm":
		r.producer = NewWebMProducer(file, 0)
	default:
		return errInvalidFile
	}
	if video {
		videoTrack, err := r.producer.GetVideoTrack()
		if err != nil {
			log.Debugf("error: %v", err)
			return err
		}
		_, err = r.pub.pc.AddTrack(videoTrack)
		if err != nil {
			log.Debugf("error: %v", err)
			return err
		}
	}
	if audio {
		audioTrack, err := r.producer.GetAudioTrack()
		if err != nil {
			log.Debugf("error: %v", err)
			return err
		}
		_, err = r.pub.pc.AddTrack(audioTrack)
		if err != nil {
			log.Debugf("error: %v", err)
			return err
		}
	}
	r.producer.Start()
	//trigger by hand
	r.onNegotiationNeeded()
	return nil
}

func (r *RTC) trackEvent(event TrackEvent) {
	if r.OnTrackEvent == nil {
		log.Errorf("r.OnTrackEvent == nil")
		return
	}
	r.OnTrackEvent(event)
}

func (r *RTC) speaker(event []string) {
	if r.OnSpeaker == nil {
		log.Errorf("r.OnSpeaker == nil")
		return
	}
	r.OnSpeaker(event)
}

// setRemoteSDP pub SetRemoteDescription and send cadidate to sfu
func (r *RTC) setRemoteSDP(sdp webrtc.SessionDescription) error {
	err := r.pub.pc.SetRemoteDescription(sdp)
	if err != nil {
		log.Errorf("id=%v err=%v", r.uid, err)
		return err
	}

	// it's safe to add cand now after SetRemoteDescription
	if len(r.pub.RecvCandidates) > 0 {
		for _, candidate := range r.pub.RecvCandidates {
			log.Debugf("id=%v r.pub.pc.AddICECandidate candidate=%v", r.uid, candidate)
			err = r.pub.pc.AddICECandidate(candidate)
			if err != nil {
				log.Errorf("id=%v r.pub.pc.AddICECandidate err=%v", r.uid, err)
			}
		}
		r.pub.RecvCandidates = []webrtc.ICECandidateInit{}
	}

	// it's safe to send cand now after join ok
	if len(r.pub.SendCandidates) > 0 {
		for _, cand := range r.pub.SendCandidates {
			log.Debugf("id=%v r.rtc.trickle cand=%v", r.uid, cand)
			r.SendTrickle(cand, Target_PUBLISHER)
		}
		r.pub.SendCandidates = []*webrtc.ICECandidate{}
	}
	return nil
}

// GetBandWidth call this api cyclely
func (r *RTC) GetBandWidth(cycle int) (int, int) {
	var recvBW, sendBW int
	if r.producer != nil {
		sendBW = r.producer.GetSendBandwidth(cycle)
	}

	recvBW = r.recvByte / cycle / 1000
	r.recvByte = 0
	return recvBW, sendBW
}

func (r *RTC) Name() string {
	return "Room"
}

func (r *RTC) Connect() {
	go r.onSingalHandleOnce()
	r.connected = true
}

func (r *RTC) Connected() bool {
	return r.connected
}

func (r *RTC) onSingalHandleOnce() {
	// onSingalHandle is wrapped in a once and only started after another public
	// method is called to ensure the user has the opportunity to register handlers
	r.handleOnce.Do(func() {
		err := r.onSingalHandle()
		if r.OnError != nil {
			r.OnError(err)
		}
	})
}

func (r *RTC) onSingalHandle() error {
	for {
		//only one goroutine for recving from stream, no need to lock
		stream, err := r.signaller.Recv()
		if err != nil {
			if err == io.EOF {
				log.Infof("[%v] WebRTC Transport Closed", r.uid)
				if err := r.signaller.CloseSend(); err != nil {
					log.Errorf("[%v] error sending close: %s", r.uid, err)
				}
				return err
			}

			errStatus, _ := status.FromError(err)
			if errStatus.Code() == codes.Canceled {
				if err := r.signaller.CloseSend(); err != nil {
					log.Errorf("[%v] error sending close: %s", r.uid, err)
				}
				return err
			}

			log.Errorf("[%v] Error receiving RTC response: %v", r.uid, err)
			if r.OnError != nil {
				r.OnError(err)
			}
			return err
		}

		switch payload := stream.Payload.(type) {
		case *rtc.Reply_Join:
			success := payload.Join.Success
			err := errors.New(payload.Join.Error.String())

			if !success {
				log.Errorf("[%v] [join] failed error: %v", r.uid, err)
				return err
			}
			log.Infof("[%v] [join] success", r.uid)
			log.Infof("payload.Reply.Description=%v", payload.Join.Description)
			sdp := webrtc.SessionDescription{
				Type: webrtc.SDPTypeAnswer,
				SDP:  payload.Join.Description.Sdp,
			}

			if err = r.setRemoteSDP(sdp); err != nil {
				log.Errorf("[%v] [join] error %s", r.uid, err)
				return err
			}
		case *rtc.Reply_Description:
			var sdpType webrtc.SDPType
			if payload.Description.Type == "offer" {
				sdpType = webrtc.SDPTypeOffer
			} else {
				sdpType = webrtc.SDPTypeAnswer
			}
			sdp := webrtc.SessionDescription{
				SDP:  payload.Description.Sdp,
				Type: sdpType,
			}
			if sdp.Type == webrtc.SDPTypeOffer {
				log.Infof("[%v] [description] got offer call s.OnNegotiate sdp=%+v", r.uid, sdp)
				err := r.negotiate(sdp)
				if err != nil {
					log.Errorf("error: %v", err)
				}
			} else if sdp.Type == webrtc.SDPTypeAnswer {
				log.Infof("[%v] [description] got answer call sdp=%+v", r.uid, sdp)
				err = r.setRemoteSDP(sdp)
				if err != nil {
					log.Errorf("[%v] [description] setRemoteSDP err=%s", r.uid, err)
				}
			}
		case *rtc.Reply_Trickle:
			var candidate webrtc.ICECandidateInit
			_ = json.Unmarshal([]byte(payload.Trickle.Init), &candidate)
			log.Infof("[%v] [trickle] type=%v candidate=%v", r.uid, payload.Trickle.Target, candidate)
			r.trickle(candidate, Target(payload.Trickle.Target))
		case *rtc.Reply_TrackEvent:
			if r.OnTrackEvent == nil {
				log.Errorf("s.OnTrackEvent == nil")
				continue
			}
			var TrackInfos []*TrackInfo
			for _, v := range payload.TrackEvent.Tracks {
				TrackInfos = append(TrackInfos, &TrackInfo{
					Id:        v.Id,
					Kind:      v.Kind,
					Muted:     v.Muted,
					Type:      MediaType(v.Type),
					StreamId:  v.StreamId,
					Label:     v.Label,
					Width:     v.Width,
					Height:    v.Height,
					FrameRate: v.FrameRate,
					Layer:     v.Layer,
				})
			}
			trackEvent := TrackEvent{
				State:  TrackEvent_State(payload.TrackEvent.State),
				Uid:    payload.TrackEvent.Uid,
				Tracks: TrackInfos,
			}

			log.Infof("s.OnTrackEvent trackEvent=%+v", trackEvent)
			r.OnTrackEvent(trackEvent)
		case *rtc.Reply_Subscription:
			if !payload.Subscription.Success {
				log.Errorf("suscription error: %v", payload.Subscription.Error)
			}
		case *rtc.Reply_Error:
			log.Errorf("Request error: %v", payload.Error)
		default:
			log.Errorf("Unknown RTC type!!!!%v", payload)
		}
	}
}

func (r *RTC) SendJoin(sid string, uid string, offer webrtc.SessionDescription, config map[string]string) error {
	log.Infof("[C=>S] [%v] sid=%v", r.uid, sid)
	go r.onSingalHandleOnce()
	r.Lock()
	err := r.signaller.Send(
		&rtc.Request{
			Payload: &rtc.Request_Join{
				Join: &rtc.JoinRequest{
					Sid:    sid,
					Uid:    uid,
					Config: config,
					Description: &rtc.SessionDescription{
						Target: rtc.Target_PUBLISHER,
						Type:   "offer",
						Sdp:    offer.SDP,
					},
				},
			},
		},
	)
	r.Unlock()
	if err != nil {
		log.Errorf("[C=>S] [%v] err=%v", r.uid, err)
	}
	return err
}

func (r *RTC) SendTrickle(candidate *webrtc.ICECandidate, target Target) {
	log.Debugf("[C=>S] [%v] candidate=%v target=%v", r.uid, candidate, target)
	bytes, err := json.Marshal(candidate.ToJSON())
	if err != nil {
		log.Errorf("error: %v", err)
		return
	}
	go r.onSingalHandleOnce()
	r.Lock()
	err = r.signaller.Send(
		&rtc.Request{
			Payload: &rtc.Request_Trickle{
				Trickle: &rtc.Trickle{
					Target: rtc.Target(target),
					Init:   string(bytes),
				},
			},
		},
	)
	r.Unlock()
	if err != nil {
		log.Errorf("[%v] err=%v", r.uid, err)
	}
}

func (r *RTC) SendOffer(sdp webrtc.SessionDescription) error {
	log.Infof("[C=>S] [%v] sdp=%v", r.uid, sdp)
	go r.onSingalHandleOnce()
	r.Lock()
	err := r.signaller.Send(
		&rtc.Request{
			Payload: &rtc.Request_Description{
				Description: &rtc.SessionDescription{
					Target: rtc.Target_PUBLISHER,
					Type:   "offer",
					Sdp:    sdp.SDP,
				},
			},
		},
	)
	r.Unlock()
	if err != nil {
		log.Errorf("[%v] err=%v", r.uid, err)
		return err
	}
	return nil
}

func (r *RTC) SendAnswer(sdp webrtc.SessionDescription) error {
	log.Infof("[C=>S] [%v] sdp=%v", r.uid, sdp)
	r.Lock()
	err := r.signaller.Send(
		&rtc.Request{
			Payload: &rtc.Request_Description{
				Description: &rtc.SessionDescription{
					Target: rtc.Target_SUBSCRIBER,
					Type:   "answer",
					Sdp:    sdp.SDP,
				},
			},
		},
	)
	r.Unlock()
	if err != nil {
		log.Errorf("[%v] err=%v", r.uid, err)
		return err
	}
	return nil
}

// Subscribe to tracks
func (r *RTC) Subscribe(trackInfos []*Subscription) error {
	if len(trackInfos) == 0 {
		return errors.New("track id is empty")
	}
	var infos []*rtc.Subscription
	for _, t := range trackInfos {
		infos = append(infos, &rtc.Subscription{
			TrackId:   t.TrackId,
			Mute:      t.Mute,
			Subscribe: t.Subscribe,
			Layer:     t.Layer,
		})
	}

	log.Infof("[C=>S] infos: %v", infos)
	err := r.signaller.Send(
		&rtc.Request{
			Payload: &rtc.Request_Subscription{
				Subscription: &rtc.SubscriptionRequest{
					Subscriptions: infos,
				},
			},
		},
	)
	return err
}

// SubscribeFromEvent will parse event and subscribe what you want
func (r *RTC) SubscribeFromEvent(event TrackEvent, audio, video bool, layer string) error {
	log.Infof("event=%+v audio=%v video=%v layer=%v", event, audio, video, layer)
	if event.State == TrackEvent_UPDATE {
		return nil
	}

	var sub bool
	if event.State == TrackEvent_ADD {
		sub = true
	}

	var infos []*Subscription
	for _, t := range event.Tracks {
		// sub audio or not
		if audio && t.Kind == "audio" {
			infos = append(infos, &Subscription{
				TrackId:   t.Id,
				Mute:      t.Muted,
				Subscribe: sub,
				Layer:     t.Layer,
			})
			continue
		}
		// sub one layer
		if layer != "" && t.Kind == "video" && t.Layer == layer {
			infos = append(infos, &Subscription{
				TrackId:   t.Id,
				Mute:      t.Muted,
				Subscribe: sub,
				Layer:     t.Layer,
			})
			continue
		}
		// sub all if not set simulcast
		if t.Kind == "video" && layer == "" {
			infos = append(infos, &Subscription{
				TrackId:   t.Id,
				Mute:      t.Muted,
				Subscribe: sub,
				Layer:     t.Layer,
			})
		}
	}
	// sub video if publisher event not setting simulcast layer
	if len(infos) == 1 {
		for _, t := range event.Tracks {
			if t.Kind == "video" {
				infos = append(infos, &Subscription{
					TrackId:   t.Id,
					Mute:      t.Muted,
					Subscribe: sub,
					Layer:     t.Layer,
				})
			}
		}
	}
	for _, i := range infos {
		log.Infof("Subscribe/UnSubscribe infos=%+v", i)
	}
	return r.Subscribe(infos)
}

// Close client close
func (r *RTC) Close() {
	log.Infof("id=%v", r.uid)
	close(r.notify)
	if r.pub != nil {
		r.pub.pc.Close()
	}
	if r.sub != nil {
		r.sub.pc.Close()
	}
	r.cancel()
}
