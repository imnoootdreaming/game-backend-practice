package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// frameOf 把一段 JSON body 手动包成"4 字节大端长度 + body"的帧，
// 用来构造正常路径之外的特殊帧（未知类型、空 body、越界长度等）
func frameOf(body []byte) []byte {
	frame := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(body)))
	copy(frame[4:], body)
	return frame
}

// TestEncodeDecodeRoundTrip 正常路径：每种消息编码后再解码，类型和字段都必须原样还原
func TestEncodeDecodeRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		wantType string
	}{
		{"LoginReq", &LoginReq{PlayerID: "p1", Nickname: "小王"}, "LoginReq"},
		{"LoginResp", &LoginResp{Code: 0, PlayerID: "p1", Token: "t-1", ExpiresAt: 1893456000}, "LoginResp"},
		{"ReadyReq", &ReadyReq{PlayerID: "p1", Ready: true}, "ReadyReq"},
		{"TeamInvite", &TeamInvite{TeamID: "team-1", From: "p1", To: "p2", Accept: true}, "TeamInvite"},
		{"MatchStart", &MatchStart{MatchID: "m1", PlayerIDs: []string{"p1", "p2", "p3", "p4"}}, "MatchStart"},
		{"RoomAssign", &RoomAssign{RoomID: "r1", PlayerIDs: []string{"p1", "p2"}, TickHz: 20}, "RoomAssign"},
		{"InputCmd", &InputCmd{PlayerID: "p1", Seq: 7, MoveX: 1.5, MoveY: -2.5, Fire: true, Timestamp: 1700000000123}, "InputCmd"},
		{"Snapshot", &Snapshot{RoomID: "r1", Tick: 42, Players: []PlayerState{{ID: "p1", X: 1, Y: 2, HP: 100, LastSeq: 7}}}, "Snapshot"},
		{"GameResult", &GameResult{RoomID: "r1", RequestID: "req-1", WinnerID: "p1", Scores: map[string]int32{"p1": 3}, EndedAt: 1700000000}, "GameResult"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := Encode(tt.msg)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if len(frame) < 4 {
				t.Fatalf("frame length = %d, want at least 4 (length prefix)", len(frame))
			}
			got, err := Decode(frame)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got.Type() != tt.wantType {
				t.Errorf("Type() = %q, want %q", got.Type(), tt.wantType)
			}
			if !reflect.DeepEqual(got, tt.msg) {
				t.Errorf("round trip = %#v, want %#v", got, tt.msg)
			}
		})
	}
}

// TestEncoderWritesFrame 正常路径：流式 Encoder 写出的字节与 Encode 一致，且能被 Decoder 读回
func TestEncoderWritesFrame(t *testing.T) {
	want, err := Encode(&ReadyReq{PlayerID: "p1", Ready: true})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(&ReadyReq{PlayerID: "p1", Ready: true}); err != nil {
		t.Fatalf("Encoder.Encode() error = %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("written bytes = %x, want %x", buf.Bytes(), want)
	}
	got, err := NewDecoder(&buf).ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	if _, ok := got.(*ReadyReq); !ok {
		t.Errorf("message = %#v, want *ReadyReq", got)
	}
}

// TestDecoderReadsTwoFramesInOneRead 粘包：一次 Read 拿到两帧，连续两次 ReadMessage 应各取一帧
func TestDecoderReadsTwoFramesInOneRead(t *testing.T) {
	first, err := Encode(&LoginReq{PlayerID: "p1", Nickname: "A"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encode(&ReadyReq{PlayerID: "p1", Ready: true})
	if err != nil {
		t.Fatal(err)
	}

	buf := bytes.NewBuffer(append(append([]byte{}, first...), second...))
	dec := NewDecoder(buf)

	msg1, err := dec.ReadMessage()
	if err != nil {
		t.Fatalf("first ReadMessage() error = %v", err)
	}
	if got, ok := msg1.(*LoginReq); !ok || got.PlayerID != "p1" {
		t.Fatalf("first message = %#v, want *LoginReq{p1}", msg1)
	}

	msg2, err := dec.ReadMessage()
	if err != nil {
		t.Fatalf("second ReadMessage() error = %v", err)
	}
	if got, ok := msg2.(*ReadyReq); !ok || !got.Ready {
		t.Fatalf("second message = %#v, want *ReadyReq{Ready:true}", msg2)
	}

	if _, err := dec.ReadMessage(); !errors.Is(err, io.EOF) {
		t.Errorf("third ReadMessage() error = %v, want io.EOF", err)
	}
}

// TestDecoderWaitsForPartialFrame 拆包：只写半帧时应报告"还没读够"，补齐后能正常解出消息
func TestDecoderWaitsForPartialFrame(t *testing.T) {
	frame, err := Encode(&Snapshot{RoomID: "r1", Tick: 9})
	if err != nil {
		t.Fatal(err)
	}
	half := len(frame) / 2

	buf := bytes.NewBuffer(frame[:half])
	dec := NewDecoder(buf)
	if _, err := dec.ReadMessage(); !errors.Is(err, io.EOF) {
		t.Fatalf("partial ReadMessage() error = %v, want io.EOF", err)
	}

	buf.Write(frame[half:])
	got, err := dec.ReadMessage()
	if err != nil {
		t.Fatalf("completed ReadMessage() error = %v", err)
	}
	snap, ok := got.(*Snapshot)
	if !ok || snap.Tick != 9 {
		t.Fatalf("message = %#v, want *Snapshot{Tick:9}", got)
	}
}

// TestDecodeUnknownMessageType 错误路径：注册表中不存在的 type 必须拒绝，避免静默丢弃消息
func TestDecodeUnknownMessageType(t *testing.T) {
	body := []byte(`{"type":"NoSuchMessage","payload":{}}`)
	_, err := Decode(frameOf(body))
	if err == nil || !strings.Contains(err.Error(), "unknown message type") {
		t.Fatalf("Decode() error = %v, want containing %q", err, "unknown message type")
	}
}

// TestDecodeEmptyPayload 错误路径：长度为 0 的帧、以及 type 有但 payload 为空，都要报错
func TestDecodeEmptyPayload(t *testing.T) {
	t.Run("zero length frame", func(t *testing.T) {
		if _, err := Decode(frameOf(nil)); err == nil || !strings.Contains(err.Error(), "empty payload") {
			t.Fatalf("Decode() error = %v, want containing %q", err, "empty payload")
		}
	})
	t.Run("null payload", func(t *testing.T) {
		body := []byte(`{"type":"LoginReq","payload":null}`)
		if _, err := Decode(frameOf(body)); err == nil || !strings.Contains(err.Error(), "empty payload") {
			t.Fatalf("Decode() error = %v, want containing %q", err, "empty payload")
		}
	})
}

// TestDecodeLengthOutOfRange 错误路径：长度前缀超过上限、声明长度大于实际帧长、帧短于前缀，都必须拒绝
func TestDecodeLengthOutOfRange(t *testing.T) {
	t.Run("exceeds max frame size", func(t *testing.T) {
		frame := make([]byte, 4)
		binary.BigEndian.PutUint32(frame, MaxFrameSize+1)
		if _, err := Decode(frame); err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("Decode() error = %v, want containing %q", err, "too large")
		}
	})
	t.Run("declared longer than actual", func(t *testing.T) {
		frame := frameOf([]byte(`{"type":"LoginReq","payload":{}}`))
		binary.BigEndian.PutUint32(frame[:4], uint32(len(frame)+100))
		if _, err := Decode(frame); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("Decode() error = %v, want containing %q", err, "exceeds")
		}
	})
	t.Run("frame shorter than prefix", func(t *testing.T) {
		if _, err := Decode([]byte{0, 0}); err == nil || !strings.Contains(err.Error(), "too short") {
			t.Fatalf("Decode() error = %v, want containing %q", err, "too short")
		}
	})
}
