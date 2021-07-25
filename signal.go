package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	// pb "github.com/pion/ion-sfu/cmd/signal/grpc/proto"
	pb "github.com/pion/ion/proto/rtc"
	"github.com/pion/webrtc/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Signal is a wrapper of grpc
type Signal struct {
	id     string
	client pb.RTCClient
	stream pb.RTC_SignalClient

	OnNegotiate    func(webrtc.SessionDescription) error
	OnTrickle      func(candidate webrtc.ICECandidateInit, target int)
	OnSetRemoteSDP func(webrtc.SessionDescription) error
	OnError        func(error)

	OnTrackEvent func(event TrackEvent)
	OnSpeaker    func(event []string)

	ctx        context.Context
	cancel     context.CancelFunc
	handleOnce sync.Once
	sync.Mutex
}

// NewSignal create a grpc signaler
func NewSignal(addr, id string) (*Signal, error) {
	s := &Signal{}
	s.id = id
	// Set up a connection to the sfu server.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	conn, err := grpc.DialContext(ctx, addr, grpc.WithInsecure())
	if err != nil {
		log.Errorf("[%v] Connecting to sfu:%s failed: %v", s.id, addr, err)
		return nil, err
	}
	log.Infof("[%v] Connecting to sfu ok: %s", s.id, addr)

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.client = pb.NewRTCClient(conn)
	s.stream, err = s.client.Signal(s.ctx)
	if err != nil {
		log.Errorf("err=%v", err)
		return nil, err
	}
	return s, nil
}

func (s *Signal) onSignalHandleOnce() {
	// onSignalHandle is wrapped in a once and only started after another public
	// method is called to ensure the user has the opportunity to register handlers
	s.handleOnce.Do(func() {
		err := s.onSignalHandle()
		if s.OnError != nil {
			s.OnError(err)
		}
	})
}

func (s *Signal) onSignalHandle() error {
	for {
		//only one goroutine for recving from stream, no need to lock
		stream, err := s.stream.Recv()
		if err != nil {
			if err == io.EOF {
				log.Infof("[%v] WebRTC Transport Closed", s.id)
				if err := s.stream.CloseSend(); err != nil {
					log.Errorf("[%v] error sending close: %s", s.id, err)
				}
				return err
			}

			errStatus, _ := status.FromError(err)
			if errStatus.Code() == codes.Canceled {
				if err := s.stream.CloseSend(); err != nil {
					log.Errorf("[%v] error sending close: %s", s.id, err)
				}
				return err
			}

			log.Errorf("[%v] Error receiving signal response: %v", s.id, err)
			return err
		}

		switch payload := stream.Payload.(type) {
		case *pb.Signalling_Reply:
			// now not have answer in join
			success := payload.Reply.Success
			err := errors.New(payload.Reply.Error.String())
			log.Infof("success=%v err=%v", success, err)
			if !success {
				log.Errorf("[%v] [join] failed error: %v", s.id, err)
				return err
			}
			log.Infof("[%v] [join] success", s.id)
		case *pb.Signalling_Description:
			log.Infof("payload.Description==%+v", payload.Description)
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
				log.Infof("[%v] [description] got offer call s.OnNegotiate sdp=%+v", s.id, sdp)
				err := s.OnNegotiate(sdp)
				if err != nil {
					log.Errorf("err=%v", err)
				}
			} else if sdp.Type == webrtc.SDPTypeAnswer {
				log.Infof("[%v] [description] got answer call s.OnSetRemoteSDP sdp=%+v", s.id, sdp)
				err = s.OnSetRemoteSDP(sdp)
				if err != nil {
					log.Errorf("[%v] [description] s.OnSetRemoteSDP err=%s", s.id, err)
				}
			}
		case *pb.Signalling_Trickle:
			var candidate webrtc.ICECandidateInit
			_ = json.Unmarshal([]byte(payload.Trickle.Init), &candidate)
			log.Infof("[%v] [trickle] type=%v candidate=%v", s.id, payload.Trickle.Target, candidate)
			s.OnTrickle(candidate, int(payload.Trickle.Target))
		case *pb.Signalling_TrackEvent:
			state := TrackNone
			switch payload.TrackEvent.State {
			case pb.TrackEvent_ADD:
				state = TrackAdd
			case pb.TrackEvent_REMOVE:
				state = TrackRemove
			}
			var tracks []Track
			for _, track := range payload.TrackEvent.Tracks {
				var simulcast []Simulcast
				for _, s := range track.Simulcast {
					simulcast = append(simulcast, Simulcast{
						Rid:        s.Rid,
						Direction:  s.Direction,
						Parameters: s.Parameters,
					})
				}
				tracks = append(tracks, Track{
					ID:        track.Id,
					StreamID:  track.StreamId,
					Kind:      track.Kind,
					Muted:     track.Muted,
					Simulcast: simulcast,
				})
			}
			trackEvent := TrackEvent{
				State:  state,
				Uid:    payload.TrackEvent.Uid,
				Tracks: tracks,
			}
			if s.OnTrackEvent == nil {
				log.Errorf("s.OnTrackEvent == nil")
				continue
			}
			log.Infof("s.OnTrackEvent trackEvent=%+v", trackEvent)
			s.OnTrackEvent(trackEvent)
		default:
			log.Errorf("Unknow signal type!!!!%v", payload)
		}
	}
}

func (s *Signal) Join(sid string, uid string, config *JoinConfig) error {
	log.Infof("[%v] [Signal.Join] sid=%v", s.id, sid)
	go s.onSignalHandleOnce()
	s.Lock()
	if config == nil {
		config = NewJoinConfig()
	}
	err := s.stream.Send(
		&pb.Signalling{
			Payload: &pb.Signalling_Join{
				Join: &pb.JoinRequest{
					Sid: sid,
					Uid: uid,
				},
			},
		},
	)
	s.Unlock()
	if err != nil {
		log.Errorf("[%v] err=%v", s.id, err)
	}
	return err
}

func (s *Signal) Trickle(candidate *webrtc.ICECandidate, target int) {
	log.Infof("[%v] [Signal.Trickle] candidate=%v target=%v", s.id, candidate, target)
	bytes, err := json.Marshal(candidate.ToJSON())
	if err != nil {
		log.Errorf("err=%v", err)
		return
	}
	go s.onSignalHandleOnce()
	s.Lock()
	err = s.stream.Send(
		&pb.Signalling{
			Payload: &pb.Signalling_Trickle{
				Trickle: &pb.Trickle{
					Target: pb.Target_PUBLISHER,
					Init:   string(bytes),
				},
			},
		},
	)
	s.Unlock()
	if err != nil {
		log.Errorf("[%v] err=%v", s.id, err)
	}
}

func (s *Signal) Offer(sdp webrtc.SessionDescription) error {
	log.Infof("[%v] [Signal.Offer] sdp=%v", s.id, sdp)
	go s.onSignalHandleOnce()
	s.Lock()
	err := s.stream.Send(
		&pb.Signalling{
			Payload: &pb.Signalling_Description{
				Description: &pb.SessionDescription{
					Target: pb.Target_PUBLISHER,
					Type:   "offer",
					Sdp:    sdp.SDP,
				},
			},
		},
	)
	s.Unlock()
	if err != nil {
		log.Errorf("[%v] err=%v", s.id, err)
		return err
	}
	return nil
}

func (s *Signal) Answer(sdp webrtc.SessionDescription) error {
	log.Infof("[%v] [Signal.Answer] sdp=%v", s.id, sdp)
	s.Lock()
	err := s.stream.Send(
		&pb.Signalling{
			Payload: &pb.Signalling_Description{
				Description: &pb.SessionDescription{
					Target: pb.Target_SUBSCRIBER,
					Type:   "answer",
					Sdp:    sdp.SDP,
				},
			},
		},
	)
	s.Unlock()
	if err != nil {
		log.Errorf("[%v] err=%v", s.id, err)
		return err
	}
	return nil
}

// Subscribe to tracks
func (s *Signal) Subscribe(trackIds []string, enable bool) error {
	if len(trackIds) == 0 {
		return errors.New("track id is empty")
	}
	err := s.stream.Send(
		&pb.Signalling{
			Payload: &pb.Signalling_UpdateSettings{
				UpdateSettings: &pb.UpdateSettings{
					Command: &pb.UpdateSettings_Subcription{
						Subcription: &pb.Subscription{
							TrackIds:  trackIds,
							Subscribe: enable,
						},
					},
				},
			},
		},
	)
	return err
}

func (s *Signal) TrackEvent(event TrackEvent) {

}

func (s *Signal) Close() {
	log.Infof("[%v] [Signal.Close]", s.id)
	s.cancel()
	go s.onSignalHandleOnce()
}
