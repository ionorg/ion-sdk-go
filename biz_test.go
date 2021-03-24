package engine

import (
	"sync"
	"testing"

	log "github.com/pion/ion-log"
	"github.com/stretchr/testify/assert"
)

const (
	bizAddr = "127.0.0.1:5551"
	sid     = "sid01"
	uid     = "myuid"
)

var (
	info = map[string]interface{}{"name": "bizclient"}
	msg  = map[string]interface{}{"text": "hello from go"}
)

var (
	bizcli *BizClient
	wg     *sync.WaitGroup
)

func init() {
	fixByFile := []string{"asm_amd64.s", "proc.go"}
	fixByFunc := []string{}
	log.Init("debug", fixByFile, fixByFunc)

	wg = new(sync.WaitGroup)

	bizcli = NewBizClient(bizAddr)

	bizcli.OnError = func(err error) {
		log.Errorf("OnError %v", err)
	}

	bizcli.OnJoin = func(success bool, reason string) {
		log.Infof("OnJoin success = %v, reason = %v", success, reason)
		wg.Done()
	}

	bizcli.OnLeave = func(reason string) {
		log.Infof("OnLeave reason = %v", reason)
		wg.Done()
	}

	bizcli.OnPeerEvent = func(state PeerState, peer Peer) {
		log.Infof("OnPeerEvent peer = %v, state = %v", peer, state)
	}

	bizcli.OnStreamEvent = func(state StreamState, sid string, uid string, streams []*Stream) {
		log.Infof("StreamEvent state = %v, sid = %v, uid = %v, streams = %v",
			state,
			sid,
			uid,
			streams)
	}
}

func TestBizJoin(t *testing.T) {

	wg.Add(1)
	err := bizcli.Join(sid, uid, info)
	if err != nil {
		t.Error(err)
	}
	wg.Wait()
}

func TestBizMessageSend(t *testing.T) {
	bizcli.OnMessage = func(from string, to string, data map[string]interface{}) {
		log.Infof("OnMessage msg = %v", data)
		assert.Equal(t, uid, from)
		assert.Equal(t, uid, to)
		assert.Equal(t, msg, data)
		wg.Done()
	}
	wg.Add(1)
	err := bizcli.SendMessage(uid, uid, msg)
	if err != nil {
		t.Error(err)
	}
	wg.Wait()
}

func TestBizLeave(t *testing.T) {
	wg.Add(1)
	err := bizcli.Leave(uid)
	if err != nil {
		t.Error(err)
	}
	wg.Wait()
}
