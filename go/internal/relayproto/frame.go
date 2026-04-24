package relayproto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
)

const (
	FrameTypeTerminalOutput byte = 1
	FrameTypeTerminalInput  byte = 2
	FrameTypeSnapshotChunk  byte = 3
)

var frameMagic = []byte("TMX1")

type BinaryFrame struct {
	FrameType byte
	Header    map[string]any
	Payload   []byte
}

func EncodeBinaryFrame(frame BinaryFrame) ([]byte, error) {
	header, err := json.Marshal(frame.Header)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 10+len(header)+len(frame.Payload))
	copy(buf[:4], frameMagic)
	buf[4] = 1
	buf[5] = frame.FrameType
	binary.BigEndian.PutUint32(buf[6:10], uint32(len(header)))
	copy(buf[10:10+len(header)], header)
	copy(buf[10+len(header):], frame.Payload)
	return buf, nil
}

func DecodeBinaryFrame(data []byte) (BinaryFrame, error) {
	if len(data) < 10 || string(data[:4]) != "TMX1" {
		return BinaryFrame{}, errors.New("invalid frame magic")
	}

	headerLen := binary.BigEndian.Uint32(data[6:10])
	if len(data) < 10+int(headerLen) {
		return BinaryFrame{}, errors.New("truncated frame header")
	}

	var header map[string]any
	if err := json.Unmarshal(data[10:10+headerLen], &header); err != nil {
		return BinaryFrame{}, err
	}
	return BinaryFrame{
		FrameType: data[5],
		Header:    header,
		Payload:   data[10+headerLen:],
	}, nil
}

func HeaderString(header map[string]any, key string) string {
	value, ok := header[key]
	if !ok {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return s
}

func HeaderInt64(header map[string]any, key string) int64 {
	value, ok := header[key]
	if !ok {
		return 0
	}

	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}
