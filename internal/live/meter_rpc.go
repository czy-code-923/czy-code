package live

import (
	"context"
	"errors"
	"net"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protowire"
)

// The bounded codec handles only the official protobuf messages consumed here.
// It avoids linking entire proxy cores into the Agent and never accepts RPC methods from the API.
type rawProto struct{}

func (rawProto) Name() string { return "proto" }
func (rawProto) Marshal(v any) ([]byte, error) {
	b, ok := v.([]byte)
	if !ok {
		return nil, errors.New("invalid RPC request")
	}
	return b, nil
}
func (rawProto) Unmarshal(b []byte, v any) error {
	p, ok := v.(*[]byte)
	if !ok {
		return errors.New("invalid RPC response")
	}
	if len(b) > 4<<20 {
		return errors.New("RPC response too large")
	}
	*p = append((*p)[:0], b...)
	return nil
}

type pbField struct {
	tag    protowire.Number
	bytes  []byte
	number uint64
}

func pbFields(b []byte) ([]pbField, error) {
	fields := []pbField{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, errors.New("malformed protobuf")
		}
		b = b[n:]
		f := pbField{tag: num}
		switch typ {
		case protowire.BytesType:
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil, errors.New("malformed protobuf bytes")
			}
			f.bytes = v
			b = b[n:]
		case protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return nil, errors.New("malformed protobuf integer")
			}
			f.number = v
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return nil, errors.New("malformed protobuf field")
			}
			b = b[n:]
		}
		fields = append(fields, f)
		if len(fields) > 20000 {
			return nil, errors.New("too many protobuf fields")
		}
	}
	return fields, nil
}
func rpcConnection(port int) (*grpc.ClientConn, error) {
	target := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	return grpc.NewClient("passthrough:///"+target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", target)
	}), grpc.WithDefaultCallOptions(grpc.ForceCodec(rawProto{}), grpc.MaxCallRecvMsgSize(4<<20)))
}
func secretContext(ctx context.Context, secret string) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+secret))
}
