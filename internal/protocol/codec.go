package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"

	proto "github.com/golang/protobuf/proto"

	sessionv1 "meshserver/internal/gen/proto/meshserver/session/v1"
)

const currentEnvelopeVersion = 1

// MaxEnvelopeBytes caps a single length-prefixed envelope payload to limit memory use (DoS).
const MaxEnvelopeBytes = 8 << 20 // 8 MiB

// ErrEnvelopeTooLarge is returned when an envelope payload exceeds MaxEnvelopeBytes.
var ErrEnvelopeTooLarge = fmt.Errorf("envelope payload exceeds %d bytes", MaxEnvelopeBytes)

// EnvelopeCodec reads and writes length-prefixed protobuf envelopes.
type EnvelopeCodec struct{}

// ReadEnvelope reads one envelope from a stream.
func ReadEnvelope(r io.Reader) (*sessionv1.Envelope, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}
	if length > MaxEnvelopeBytes {
		return nil, fmt.Errorf("%w: got %d", ErrEnvelopeTooLarge, length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	var env sessionv1.Envelope
	if err := proto.Unmarshal(payload, &env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return &env, nil
}

// WriteEnvelope writes one envelope to a stream.
func WriteEnvelope(w io.Writer, env *sessionv1.Envelope) error {
	payload, err := proto.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	if len(payload) > MaxEnvelopeBytes {
		return fmt.Errorf("marshal envelope: %w", ErrEnvelopeTooLarge)
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(payload))); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

// MarshalBody serializes a protobuf message.
func MarshalBody(msg proto.Message) ([]byte, error) {
	return proto.Marshal(msg)
}

// UnmarshalBody deserializes a protobuf message.
func UnmarshalBody(data []byte, msg proto.Message) error {
	return proto.Unmarshal(data, msg)
}

// NewEnvelope builds an envelope around a protobuf message body.
func NewEnvelope(msgType sessionv1.MsgType, requestID string, body proto.Message) (*sessionv1.Envelope, error) {
	payload, err := MarshalBody(body)
	if err != nil {
		return nil, err
	}
	return &sessionv1.Envelope{
		Version:     currentEnvelopeVersion,
		MsgType:     msgType,
		RequestId:   requestID,
		TimestampMs: uint64(time.Now().UTC().UnixMilli()),
		Body:        payload,
	}, nil
}
