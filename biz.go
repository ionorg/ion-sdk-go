package engine

import (
	"context"
	"io"
	"sync"

	"github.com/pion/ion-sdk-go/pkg/grpc/biz"
	"github.com/pion/ion-sdk-go/pkg/grpc/ion"
	"github.com/square/go-jose/v3/json"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	OnMessage     func(from string, to string, data map[string]interface{})
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

func (c *BizClient) Join(sid string, uid string, info map[string]interface{}) error {
	log.Infof("[Biz.Join] sid=%v uid=%v, info=%v", sid, uid, info)
	buf, err := json.Marshal(info)
	if err != nil {
		log.Errorf("Marshal join.info [%v] err=%v", sid, err)
		c.OnError(err)
		return err
	}
	err = c.stream.Send(
		&biz.SignalRequest{
			Payload: &biz.SignalRequest_Join{
				Join: &biz.Join{
					Peer: &ion.Peer{
						Sid:  sid,
						Uid:  uid,
						Info: buf,
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

func (c *BizClient) SendMessage(from string, to string, data map[string]interface{}) error {
	log.Infof("[Biz.SendMessage] from=%v, to=%v, data=%v", from, to, data)
	buf, err := json.Marshal(data)
	if err != nil {
		log.Errorf("Marshal msg.data [%v] err=%v", from, err)
		c.OnError(err)
		return err
	}
	err = c.stream.Send(
		&biz.SignalRequest{
			Payload: &biz.SignalRequest_Msg{
				Msg: &ion.Message{
					From: from,
					To:   to,
					Data: buf,
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

		// switch payload := res.Payload.(type) {
		// case *biz.SignalReply_JoinReply:
		// reply := payload.JoinReply
		// if c.OnJoin != nil {
		// c.OnJoin(reply.Success, reply.GetReason())
		// }
		// case *biz.SignalReply_LeaveReply:
		// reply := payload.LeaveReply
		// if c.OnLeave != nil {
		// c.OnLeave(reply.Reason)
		// }
		// case *biz.SignalReply_Msg:
		// msg := payload.Msg
		// data := make(map[string]interface{})

		// err := json.Unmarshal(msg.Data, &data)
		// if err != nil {
		// log.Errorf("Unmarshal peer.info: err %v", err)
		// c.OnError(err)
		// return err
		// }

		// if c.OnMessage != nil {
		// c.OnMessage(msg.From, msg.To, data)
		// }
		// case *biz.SignalReply_PeerEvent:
		// event := payload.PeerEvent
		// info := make(map[string]interface{})

		// if event.State == ion.PeerEvent_JOIN ||
		// event.State == ion.PeerEvent_UPDATE {
		// err := json.Unmarshal(event.Peer.Info, &info)
		// if err != nil {
		// log.Errorf("Unmarshal peer.info: err %v", err)
		// c.OnError(err)
		// return err
		// }
		// }

		// if c.OnPeerEvent != nil {
		// c.OnPeerEvent(PeerState(event.State), Peer{
		// Sid:  event.Peer.Sid,
		// Uid:  event.Peer.Uid,
		// Info: info,
		// })
		// }
		// case *biz.SignalReply_StreamEvent:
		// event := payload.StreamEvent
		// if c.OnStreamEvent != nil {
		// var streams []*Stream
		// for _, st := range event.Streams {
		// stream := &Stream{
		// Id: st.Id,
		// }
		// for _, t := range st.Tracks {
		// track := &Track{
		// Id:        t.Id,
		// Label:     t.Label,
		// Kind:      t.Kind,
		// Simulcast: t.Simulcast,
		// }
		// stream.Tracks = append(stream.Tracks, track)
		// }
		// streams = append(streams, stream)
		// }
		// c.OnStreamEvent(StreamState(event.State), event.Sid, event.Uid, streams)
		// }
		// default:
		// break
		// }
	}
}
