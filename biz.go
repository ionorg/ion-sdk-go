package engine

import (
	"context"
	"io"
	"sync"

	log "github.com/pion/ion-log"
	"github.com/pion/ion-sdk-go/pkg/grpc/biz"
	"github.com/pion/ion-sdk-go/pkg/grpc/ion"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type PeerState int32

const (
	PeerJOIN   PeerState = 0
	PeerUPDATE PeerState = 1
	PeerLEAVE  PeerState = 2
)

type Peer struct {
	Sid  string
	Uid  string
	Info []byte
}

type Track struct {
	Id        string
	Label     string
	Kind      string
	Simulcast map[string]string
}

type Stream struct {
	Id     string
	Tracks []*Track
}

type StreamState int32

const (
	StreamADD    StreamState = 0
	StreamREMOVE StreamState = 2
)

type BizClient struct {
	client biz.BizClient
	stream biz.Biz_SignalClient
	ctx    context.Context
	cancel context.CancelFunc
	sync.Mutex

	OnJoin        func(success bool, reason string)
	OnLeave       func(reason string)
	OnPeerEvent   func(state PeerState, peer Peer)
	OnStreamEvent func(state StreamState, sid string, uid string, streams []*Stream)
	OnMessage     func(from string, to string, data []byte)
	OnError       func(error)
}

func NewBizClient(addr string) *BizClient {
	c := &BizClient{}
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Errorf("did not connect: %v", err)
		return nil
	}
	log.Infof("gRPC connected: %s", addr)

	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.client = biz.NewBizClient(conn)
	c.stream, err = c.client.Signal(c.ctx)
	if err != nil {
		log.Errorf("err=%v", err)
		return nil
	}
	go c.bizSignalReadLoop()
	return c
}

func (c *BizClient) Close() {
	log.Infof("[Biz.Close]")
	c.cancel()
	c.stream.CloseSend()
}

func (c *BizClient) Join(sid string, uid string, info []byte) error {
	log.Infof("[Biz.Join] sid=%v uid=%v, info=%v", sid, uid, info)
	err := c.stream.Send(
		&biz.SignalRequest{
			Payload: &biz.SignalRequest_Join{
				Join: &biz.Join{
					Peer: &ion.Peer{
						Sid:  sid,
						Uid:  uid,
						Info: info,
					},
				},
			},
		},
	)
	if err != nil {
		log.Errorf("[%v] err=%v", sid, err)
		c.OnError(err)
		return err
	}

	return nil
}

func (c *BizClient) Leave(uid string) error {
	log.Infof("[Biz.Leave] uid=%v", uid)
	err := c.stream.Send(
		&biz.SignalRequest{
			Payload: &biz.SignalRequest_Leave{
				Leave: &biz.Leave{
					Uid: uid,
				},
			},
		},
	)
	if err != nil {
		log.Errorf("[%v] err=%v", uid, err)
		c.OnError(err)
		return err
	}
	return nil
}

func (c *BizClient) SendMessage(from string, to string, data []byte) error {
	log.Infof("[Biz.SendMessage] from=%v, to=%v, data=%v", from, to, data)
	err := c.stream.Send(
		&biz.SignalRequest{
			Payload: &biz.SignalRequest_Msg{
				Msg: &ion.Message{
					From: from,
					To:   to,
					Data: data,
				},
			},
		},
	)
	if err != nil {
		log.Errorf("err=%v", err)
		c.OnError(err)
		return err
	}

	return nil
}

func (c *BizClient) bizSignalReadLoop() error {
	for {
		res, err := c.stream.Recv()
		if err != nil {
			if err == io.EOF {
				if err := c.stream.CloseSend(); err != nil {
					log.Errorf("error sending close: %s", err)
				}
				c.OnError(err)
				return err
			}

			errStatus, _ := status.FromError(err)
			if errStatus.Code() == codes.Canceled {
				if err := c.stream.CloseSend(); err != nil {
					log.Errorf("error sending close: %s", err)
				}
				c.OnError(err)
				return err
			}

			log.Errorf("Error receiving biz response: %v", err)
			c.OnError(err)
			return err
		}

		log.Debugf("BizClient.bizSignalReadLoop reply %v", res)

		switch payload := res.Payload.(type) {
		case *biz.SignalReply_JoinReply:
			reply := payload.JoinReply
			if c.OnJoin != nil {
				c.OnJoin(reply.Success, reply.GetReason())
			}
		case *biz.SignalReply_LeaveReply:
			if c.OnLeave != nil {
				c.OnLeave("")
			}
		case *biz.SignalReply_Msg:
			msg := payload.Msg
			if c.OnMessage != nil {
				c.OnMessage(msg.From, msg.To, msg.Data)
			}
		case *biz.SignalReply_PeerEvent:
			event := payload.PeerEvent
			if c.OnPeerEvent != nil {
				c.OnPeerEvent(PeerState(event.State), Peer{
					Sid:  event.Peer.Sid,
					Uid:  event.Peer.Uid,
					Info: event.Peer.Info,
				})
			}
		case *biz.SignalReply_StreamEvent:
			event := payload.StreamEvent
			if c.OnStreamEvent != nil {
				var streams []*Stream
				for _, st := range event.Streams {
					stream := &Stream{
						Id: st.Id,
					}
					for _, t := range st.Tracks {
						track := &Track{
							Id:        t.Id,
							Label:     t.Label,
							Kind:      t.Kind,
							Simulcast: t.Simulcast,
						}
						stream.Tracks = append(stream.Tracks, track)
					}
					streams = append(streams, stream)
				}
				c.OnStreamEvent(StreamState(event.State), event.Sid, event.Uid, streams)
			}
		default:
			break
		}
	}
}
