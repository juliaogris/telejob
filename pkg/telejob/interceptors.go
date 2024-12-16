package telejob

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// unaryInterceptorCN is a unary interceptor that extracts the common name from
// the client's certificate and adds it to the context.
func unaryInterceptorCN(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	cn, err := extractCommonName(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "%v", err)
	}
	ctx = context.WithValue(ctx, OwnerKey{}, cn)
	return handler(ctx, req)
}

// streamInterceptorCN is a stream interceptor that extracts the common name
// from the client's certificate and adds it to the context.
func streamInterceptorCN(srv interface{}, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := stream.Context()
	cn, err := extractCommonName(ctx)
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "%v", err)
	}
	ctx = context.WithValue(ctx, OwnerKey{}, cn)
	wrapped := &wrappedServerStream{ServerStream: stream, ctx: ctx}
	return handler(srv, wrapped)
}

// extractCommonName extracts the common name from the client's certificate.
func extractCommonName(ctx context.Context) (string, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("%w: cannot get peer from context", ErrCommonName)
	}
	tlsInfo, ok := peer.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return "", fmt.Errorf("%w: cannot get TLSInfo from peer", ErrCommonName)
	}
	peerCerts := tlsInfo.State.PeerCertificates
	if len(peerCerts) == 0 {
		return "", fmt.Errorf("%w: no peer certificates", ErrCommonName)
	}
	return peerCerts[0].Subject.CommonName, nil
}

// wrappedServerStream is a wrapper around grpc.ServerStream that allows
// modifying the context.
type wrappedServerStream struct {
	grpc.ServerStream
	//nolint:containedctx
	// seems to be an accepted pattern for stream middleware see
	// https://github.com/grpc-ecosystem/go-grpc-middleware/blob/d42ae9d517069c2bd7f9339147a0eafa86b3d4a3/wrappers.go#L16
	// https://github.com/grpc/grpc-go/blob/4c07bca27377feb808912b844b3fa95ad10f946b/examples/features/interceptor/server/main.go#L109
	ctx context.Context
}

// Context returns the modified context.
func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}
