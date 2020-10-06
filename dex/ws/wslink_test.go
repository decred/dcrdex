// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"github.com/gorilla/websocket"
)

var tLogger = dex.StdOutLogger("ws_TEST", dex.LevelTrace)

type ConnStub struct {
	inMsg  chan []byte
	inErr  chan error
	closed int32
}

func (c *ConnStub) Close() error {
	// make ReadMessage return with a close error
	atomic.StoreInt32(&c.closed, 1)
	c.inErr <- &websocket.CloseError{
		Code: websocket.CloseNormalClosure,
		Text: "bye",
	}
	return nil
}
func (c *ConnStub) SetReadDeadline(t time.Time) error {
	return nil
}
func (c *ConnStub) SetWriteDeadline(t time.Time) error {
	return nil
}
func (c *ConnStub) ReadMessage() (int, []byte, error) {
	if atomic.LoadInt32(&c.closed) == 1 {
		return 0, nil, &websocket.CloseError{
			Code: websocket.CloseAbnormalClosure,
			Text: io.ErrUnexpectedEOF.Error(),
		}
	}
	select {
	case msg := <-c.inMsg:
		return len(msg), msg, nil
	case err := <-c.inErr:
		return 0, nil, err
	}
}

const (
	stdDev      = 500.0
	minInterval = int64(50)
)

func microSecDelay(stdDev float64, min int64) time.Duration {
	return time.Microsecond * time.Duration(int64(stdDev*math.Abs(rand.NormFloat64()))+min)
}

var lastID int64 = -1 // first msg.ID should be 0

func (c *ConnStub) WriteMessage(_ int, b []byte) error {
	if atomic.LoadInt32(&c.closed) == 1 {
		return websocket.ErrCloseSent
	}
	msg, err := msgjson.DecodeMessage(b)
	if err != nil {
		return err
	}
	if msg.ID != uint64(lastID+1) {
		return fmt.Errorf("sent out of sequence. got %d, want %v", msg.ID, uint64(lastID+1))
	}
	lastID++
	writeDuration := microSecDelay(stdDev, minInterval)
	time.Sleep(writeDuration)
	return nil
}
func (c *ConnStub) WriteControl(messageType int, data []byte, deadline time.Time) error {
	switch messageType {
	case websocket.PingMessage:
		fmt.Println(" <ConnStub> ping sent to peer")
	case websocket.TextMessage:
		fmt.Println(" <ConnStub> message sent to peer:", string(data))
	case websocket.CloseMessage:
		fmt.Println(" <ConnStub> close control frame sent to peer")
	default:
		fmt.Printf(" <ConnStub> message type %d sent to peer\n", messageType)
	}
	return nil
}

func TestWSLink_send(t *testing.T) {
	defer os.Stdout.Sync()

	handlerChan := make(chan struct{}, 1)
	inMsgHandler := func(msg *msgjson.Message) *msgjson.Error {
		handlerChan <- struct{}{}
		return nil
	}

	conn := &ConnStub{
		inMsg: make(chan []byte, 1),
		inErr: make(chan error, 1),
	}
	wsLink := NewWSLink("127.0.0.1", conn, time.Second, inMsgHandler, tLogger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// start the in/out/pingHandlers, and the initial read deadline
	wg, err := wsLink.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	defer wg.Wait()

	// hit the inHandler once before testing sends
	msg, _ := msgjson.NewRequest(12, "beep", "boop")
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	// give something for inHandler to read
	conn.inMsg <- b
	// ensure that the handler was called
	select {
	case <-handlerChan:
	case <-time.NewTimer(time.Second).C:
		t.Fatal("in handler not called")
	}

	// sends
	stuff := struct {
		Thing int    `json:"thing"`
		Blah  string `json:"stuff"`
	}{
		12, "asdf",
	}
	msg, _ = msgjson.NewNotification("blah", stuff)

	err = wsLink.SendNow(msg)
	if err != nil {
		t.Error(err)
	}
	msg.ID++ // not normally used for ntfns, just in this test

	// slightly slower than send rate, bouncing off of 0 queue length
	sendStdDev := stdDev * 20 / 19
	sendMin := minInterval

	sendCount := 10000
	if testing.Short() {
		sendCount = 1000
	}
	t.Logf("Sending %v messages at the same average rate as write latency.", sendCount)
	for i := 0; i < sendCount; i++ {
		err = wsLink.Send(msg)
		if err != nil {
			t.Fatal(err)
		}
		msg.ID++
		time.Sleep(microSecDelay(sendStdDev, sendMin))
	}

	// send much faster briefly, building queue up a bit to be drained on disconnect
	sendStdDev = stdDev * 4 / 5
	sendMin = minInterval / 2
	t.Logf("Sending %v messages faster than the average write latency.", sendCount)
	for i := 0; i < sendCount; i++ {
		err = wsLink.Send(msg)
		if err != nil {
			t.Fatal(err)
		}
		msg.ID++
		time.Sleep(microSecDelay(sendStdDev, sendMin))
	}

	// NOTE: The following message may be sent by the main write loop in
	// (*WSLink).outHandler, or in the deferred drain/send of queued messages.
	// _ = wsLink.Send(msg)

	wsLink.Disconnect()

	// Make like a good connection manager and wait for the wsLink to shutdown.
	wg.Wait()

	if lastID != int64(msg.ID-1) {
		t.Errorf("final message %d not sent, last ID is %d", msg.ID-1, lastID)
	}
}
