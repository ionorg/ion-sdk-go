package engine

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	log "github.com/pion/ion-log"
	pb "github.com/pion/ion-sfu/cmd/signal/grpc/proto"
	"github.com/pion/webrtc/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Signal is a wrapper of grpc
type Signal struct {
	id     string
	client pb.SFUClient
	stream pb.SFU_SignalClient

	OnNegotiate    func(webrtc.SessionDescription) error
	OnTrickle      func(candidate webrtc.ICECandidateInit, target int)
	OnSetRemoteSDP func(webrtc.SessionDescription) error
	OnError        func(error)

	ctx        context.Context
	cancel     context.CancelFunc
	handleOnce sync.Once
	sync.Mutex
}

// NewSignal create a grpc signaler
func NewSignal(addr, id string) *Signal {
	s := &Signal{}
	s.id = id
	// Set up a connection to the sfu server.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	conn, err := grpc.DialContext(ctx, addr, grpc.WithInsecure())
	if err != nil {
		log.Errorf("[%v] Connecting to sfu:%s failed: %v", s.id, addr, err)
		return nil
	}
	log.Infof("[%v] Connecting to sfu ok: %s", s.id, addr)

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.client = pb.NewSFUClient(conn)
	s.stream, err = s.client.Signal(s.ctx)
	if err != nil {
		log.Errorf("err=%v", err)
		return nil
	}
	return s
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
		res, err := s.stream.Recv()
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

		switch payload := res.Payload.(type) {
		case *pb.SignalReply_Join:
			// Set the remote SessionDescription
			log.Infof("[%v] [join] got answer: %s", s.id, payload.Join.Description)

			var sdp webrtc.SessionDescription
			err := json.Unmarshal(payload.Join.Description, &sdp)
			if err != nil {
				log.Errorf("[%v] [join] sdp unmarshal error: %v", s.id, err)
				return err
			}

			if err = s.OnSetRemoteSDP(sdp); err != nil {
				log.Errorf("[%v] [join] s.OnSetRemoteSDP error %s", s.id, err)
				return err
			}
		case *pb.SignalReply_Description:
			var sdp webrtc.SessionDescription
			err := json.Unmarshal(payload.Description, &sdp)
			if err != nil {
				log.Errorf("[%v] [description] sdp unmarshal error: %v", s.id, err)
				return err
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
		case *pb.SignalReply_Trickle:
			var candidate webrtc.ICECandidateInit
			_ = json.Unmarshal([]byte(payload.Trickle.Init), &candidate)
			log.Infof("[%v] [trickle] type=%v candidate=%v", s.id, payload.Trickle.Target, candidate)
			s.OnTrickle(candidate, int(payload.Trickle.Target))
		default:
			// log.Errorf("Unknow signal type!!!!%v", payload)
		}
	}
}

func (s *Signal) Join(sid string, offer webrtc.SessionDescription) error {
	log.Infof("[%v] [Signal.Join] sid=%v offer=%v", s.id, sid, offer)
	marshalled, err := json.Marshal(offer)
	if err != nil {
		return err
	}
	go s.onSignalHandleOnce()
	s.Lock()
	err = s.stream.Send(
		&pb.SignalRequest{
			Payload: &pb.SignalRequest_Join{
				Join: &pb.JoinRequest{
					Sid:         sid,
					Description: marshalled,
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
	err = s.stream.Send(&pb.SignalRequest{
		Payload: &pb.SignalRequest_Trickle{
			Trickle: &pb.Trickle{
				Init:   string(bytes),
				Target: pb.Trickle_Target(target),
			},
		},
	})
	s.Unlock()
	if err != nil {
		log.Errorf("[%v] err=%v", s.id, err)
	}
}

func (s *Signal) Offer(sdp webrtc.SessionDescription) {
	log.Infof("[%v] [Signal.Offer] sdp=%v", s.id, sdp)
	marshalled, err := json.Marshal(sdp)
	if err != nil {
		log.Errorf("[%v] err=%v", s.id, err)
		return
	}
	go s.onSignalHandleOnce()
	s.Lock()
	err = s.stream.Send(
		&pb.SignalRequest{
			Payload: &pb.SignalRequest_Description{
				Description: marshalled,
			},
		},
	)
	s.Unlock()
	if err != nil {
		log.Errorf("[%v] err=%v", s.id, err)
	}
}

func (s *Signal) Answer(sdp webrtc.SessionDescription) {
	log.Infof("[%v] [Signal.Answer] sdp=%v", s.id, sdp)
	marshalled, err := json.Marshal(sdp)
	if err != nil {
		log.Errorf("err=%v", err)
		return
	}
	s.Lock()
	err = s.stream.Send(
		&pb.SignalRequest{
			Payload: &pb.SignalRequest_Description{
				Description: marshalled,
			},
		},
	)
	s.Unlock()
	if err != nil {
		log.Errorf("[%v] err=%v", s.id, err)
	}
}

func (s *Signal) Close() {
	log.Infof("[%v] [Signal.Close]", s.id)
	s.cancel()
	go s.onSignalHandleOnce()
}
