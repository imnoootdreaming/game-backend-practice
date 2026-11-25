package protocol

import (
	"encoding/binary" // 长度前缀用 4 字节大端整数
	"encoding/json"   // 当前帧体用 JSON 编码
	"errors"          // 构造空消息等简单错误
	"fmt"             // 格式化带上下文的解码错误
	"io"              // 流式 Encoder/Decoder 读写连接
)

// MaxFrameSize 是单帧 JSON body 的上限（1 MiB）。
// 不设上限时，一个恶意的长度前缀就能让对端分配巨大内存，所以解码前必须先卡这个阈值。
const MaxFrameSize = 1 << 20

// lengthPrefixSize 是长度前缀占用的字节数：4 字节大端 uint32。
const lengthPrefixSize = 4

// Codec 是编解码器抽象。当前实现是 JSONCodec；阶段 3 接入 Protobuf 时
// 只要提供同样接口的实现，上层业务代码（登录、房间、路由）不用改。
type Codec interface {
	Encode(Message) ([]byte, error)
	Decode([]byte) (Message, error)
}

// JSONCodec 是"4 字节长度前缀 + JSON"的当前实现。
// 前缀解决 TCP/WebSocket 的粘包和拆包问题：读到半帧就等下一批数据，读到两帧就分两次处理。
type JSONCodec struct{}

// Encode 把一条消息编成一个完整帧：| 4 字节长度 | Envelope JSON |。
func (JSONCodec) Encode(msg Message) ([]byte, error) {
	if msg == nil {
		return nil, errors.New("protocol: nil message")
	}

	// 业务字段先编码成 RawMessage，再和 type 一起装进 Envelope
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("protocol: marshal payload %q: %w", msg.Type(), err)
	}
	body, err := json.Marshal(Envelope{Type: msg.Type(), Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("protocol: marshal envelope: %w", err)
	}
	if len(body) > MaxFrameSize {
		return nil, fmt.Errorf("protocol: frame too large: %d bytes", len(body))
	}

	frame := make([]byte, lengthPrefixSize+len(body))
	binary.BigEndian.PutUint32(frame[:lengthPrefixSize], uint32(len(body)))
	copy(frame[lengthPrefixSize:], body)
	return frame, nil
}

// Decode 解码一个完整帧。调用方要保证传入的是恰好一帧；
// 从连接读数据时应该交给下面的 Decoder，由它负责缓存和拆帧。
func (JSONCodec) Decode(frame []byte) (Message, error) {
	if len(frame) < lengthPrefixSize {
		return nil, fmt.Errorf("protocol: frame too short: %d bytes", len(frame))
	}

	n := binary.BigEndian.Uint32(frame[:lengthPrefixSize])
	if n > MaxFrameSize {
		return nil, fmt.Errorf("protocol: frame too large: %d bytes", n)
	}
	if n == 0 {
		return nil, errors.New("protocol: empty payload: frame body length is 0")
	}
	if uint64(len(frame)-lengthPrefixSize) < uint64(n) {
		return nil, fmt.Errorf("protocol: frame length %d exceeds actual body %d", n, len(frame)-lengthPrefixSize)
	}

	body := frame[lengthPrefixSize : lengthPrefixSize+int(n)]
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("protocol: unmarshal envelope: %w", err)
	}
	if len(env.Payload) == 0 || string(env.Payload) == "null" {
		return nil, fmt.Errorf("protocol: empty payload for type %q", env.Type)
	}

	msg, err := newMessage(env.Type)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(env.Payload, msg); err != nil {
		return nil, fmt.Errorf("protocol: unmarshal payload %q: %w", env.Type, err)
	}
	return msg, nil
}

// Encode / Decode 是包级便捷函数，等价于 JSONCodec{}.Encode/Decode，
// 让调用方不必关心当前具体用的是哪种 codec。
func Encode(msg Message) ([]byte, error)   { return JSONCodec{}.Encode(msg) }
func Decode(frame []byte) (Message, error) { return JSONCodec{}.Decode(frame) }

// Encoder 把消息写入 io.Writer，供 TCP 连接或 WebSocket 连接直接使用。
type Encoder struct {
	w io.Writer
}

// NewEncoder 基于一个 io.Writer 创建流式编码器。
func NewEncoder(w io.Writer) *Encoder { return &Encoder{w: w} }

// Encode 先编帧再一次性写出。返回错误时消息可能已部分写出，调用方应关闭连接。
func (e *Encoder) Encode(msg Message) error {
	frame, err := Encode(msg)
	if err != nil {
		return err
	}
	_, err = e.w.Write(frame)
	return err
}

// Decoder 从 io.Reader 逐帧读消息，内部缓存还没读满的字节，
// 因此天然处理 TCP/WebSocket 的粘包（一次读多帧）和拆包（一帧分多次读）。
type Decoder struct {
	r   io.Reader
	buf []byte // 已读出但尚未组成完整帧的字节
}

// NewDecoder 基于一个 io.Reader 创建流式解码器。
func NewDecoder(r io.Reader) *Decoder { return &Decoder{r: r} }

// ReadMessage 返回下一帧解出的消息。
// io.EOF 表示底层没有更多数据：可能是连接真的关了，也可能只是对端还没发完这一帧；
// 调用方在连接仍存活时应继续等待并再次调用，已缓存的半帧不会丢失。
func (d *Decoder) ReadMessage() (Message, error) {
	for {
		// 缓存里够 4 字节才能先读出长度前缀
		if len(d.buf) >= lengthPrefixSize {
			n := binary.BigEndian.Uint32(d.buf[:lengthPrefixSize])
			if n > MaxFrameSize {
				return nil, fmt.Errorf("protocol: frame too large: %d bytes", n)
			}
			if n == 0 {
				return nil, errors.New("protocol: empty payload: frame body length is 0")
			}

			frameLen := lengthPrefixSize + int(n)
			// 长度已知，缓存够一整帧就切出来解码，剩下的留到下一次
			if len(d.buf) >= frameLen {
				frame := d.buf[:frameLen]
				d.buf = d.buf[frameLen:]
				return Decode(frame)
			}
		}

		// 不够一帧就继续从底层读，每次最多读 4 KiB
		chunk := make([]byte, 4096)
		nr, err := d.r.Read(chunk)
		if nr > 0 {
			d.buf = append(d.buf, chunk[:nr]...)
		}
		if err != nil {
			return nil, err
		}
	}
}
