package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	gproto "github.com/golang/protobuf/proto"
	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	corecrypto "github.com/libp2p/go-libp2p/core/crypto"
	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	coreprotocol "github.com/libp2p/go-libp2p/core/protocol"
	discoveryrouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	ma "github.com/multiformats/go-multiaddr"

	"meshserver/internal/auth"
	sessionv1 "meshserver/internal/gen/proto/meshserver/session/v1"
	meshlibp2p "meshserver/internal/libp2p"
	"meshserver/internal/protocol"
)

func main() {
	var (
		serverAddr                    = flag.String("server-addr", "", "meshserver libp2p address, for example /ip4/127.0.0.1/tcp/4001/p2p/12D3Koo...")
		nodePeerID                    = flag.String("node-peer-id", "", "meshserver node peer id to resolve via DHT and connect")
		discover                      = flag.Bool("discover", false, "discover a meshserver peer via DHT instead of using -server-addr")
		discoverNamespace             = flag.String("discover-namespace", "meshserver", "DHT rendezvous namespace used by meshserver discovery")
		keyPath                       = flag.String("key", "examples/connect-client.key", "path to the client libp2p private key")
		clientAgent                   = flag.String("client-agent", "connect-client-go", "client agent string")
		protocolID                    = flag.String("protocol-id", "/meshserver/session/1.0.0", "session protocol id")
		showCreateSpacePermissions   = flag.Bool("create-space-permissions", false, "whether to query create-space permissions after auth")
		permissionsSpaceID            = flag.Uint64("permissions-space-id", 0, "space_id to query space permissions after auth")
		requestTimeout                = flag.Duration("timeout", 20*time.Second, "total request timeout")
	)
	var dhtBootstrapPeers bootstrapPeerFlags
	flag.Var(&dhtBootstrapPeers, "dht-bootstrap-peer", "repeatable DHT bootstrap multiaddr used for discovery")
	flag.Parse()

	if !*discover && strings.TrimSpace(*serverAddr) == "" && strings.TrimSpace(*nodePeerID) == "" {
		log.Fatal("missing -server-addr, -node-peer-id, or enable -discover")
	}

	privKey, err := meshlibp2p.LoadOrCreateIdentity(*keyPath)
	if err != nil {
		log.Fatalf("load client key: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *requestTimeout)
	defer cancel()

	host, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
	)
	if err != nil {
		log.Fatalf("create libp2p host: %v", err)
	}
	defer host.Close()

	var info *peer.AddrInfo
	if *discover || strings.TrimSpace(*nodePeerID) != "" {
		if len(dhtBootstrapPeers) == 0 {
			log.Println("no -dht-bootstrap-peer provided, using libp2p default bootstrap peers")
		}
		bootstrapPeers, err := parseBootstrapPeers(dhtBootstrapPeers)
		if err != nil {
			log.Fatalf("parse dht bootstrap peers: %v", err)
		}
		for _, bootstrapPeer := range bootstrapPeers {
			if err := connectPeer(ctx, host, bootstrapPeer); err != nil {
				log.Printf("connect dht bootstrap peer %s failed: %v", bootstrapPeer.ID.String(), err)
			}
		}
		dhtNode, err := dht.New(ctx, host, dht.Mode(dht.ModeAuto), dht.BootstrapPeers(bootstrapPeers...))
		if err != nil {
			log.Fatalf("create dht: %v", err)
		}
		defer dhtNode.Close()
		if err := dhtNode.Bootstrap(ctx); err != nil {
			log.Fatalf("bootstrap dht: %v", err)
		}
		if *discover {
			info, err = discoverMeshserverPeer(ctx, host, dhtNode, *discoverNamespace)
			if err != nil {
				log.Fatalf("discover meshserver peer: %v", err)
			}
			fmt.Printf("discovered meshserver peer: peer_id=%s addrs=%v namespace=%s\n", info.ID.String(), info.Addrs, *discoverNamespace)
		} else {
			info, err = resolveMeshserverPeerByID(ctx, host, dhtNode, *nodePeerID)
			if err != nil {
				log.Fatalf("resolve meshserver peer by id: %v", err)
			}
			fmt.Printf("resolved meshserver peer by id: peer_id=%s addrs=%v\n", info.ID.String(), info.Addrs)
		}
	} else {
		info, err = addrInfo(*serverAddr)
		if err != nil {
			log.Fatalf("parse server addr: %v", err)
		}
	}

	if err := connectPeer(ctx, host, *info); err != nil {
		log.Fatalf("connect server: %v", err)
	}
	fmt.Printf("connected server peer: peer_id=%s addrs=%v\n", info.ID.String(), info.Addrs)

	stream, err := host.NewStream(ctx, info.ID, coreprotocol.ID(*protocolID))
	if err != nil {
		log.Fatalf("open stream: %v", err)
	}
	defer stream.Close()

	authResult, err := authenticate(ctx, stream, privKey, host.ID().String(), *clientAgent, *protocolID)
	if err != nil {
		log.Fatalf("authenticate: %v", err)
	}

	fmt.Printf("authenticated: user_id=%s display_name=%s session_id=%s\n", authResult.UserId, authResult.DisplayName, authResult.SessionId)
	printSpaces(authResult.Spaces)

	if *showCreateSpacePermissions {
		resp, err := getCreateSpacePermissions(stream)
		if err != nil {
			log.Fatalf("get create-space permissions: %v", err)
		}
		fmt.Printf("create-space permissions: can_create_space=%v\n", resp.CanCreateSpace)
	}

	if *permissionsSpaceID != 0 {
		resp, err := getSpacePermissions(stream, uint32(*permissionsSpaceID))
		if err != nil {
			log.Fatalf("get space permissions: %v", err)
		}
		fmt.Printf(
			"space permissions: space_id=%d role=%s can_create_group=%v\n",
			resp.SpaceId,
			resp.Role.String(),
			resp.CanCreateGroup,
		)
	}
}

func authenticate(ctx context.Context, stream network.Stream, privKey corecrypto.PrivKey, clientPeerID string, clientAgent string, protocolID string) (*sessionv1.AuthResult, error) {
	reqID := requestID("hello")
	if err := writeEnvelope(stream, sessionv1.MsgType_HELLO, reqID, &sessionv1.Hello{
		ClientPeerId:    clientPeerID,
		ClientAgent:     clientAgent,
		ProtocolVersion: "1.0.0",
	}); err != nil {
		return nil, err
	}

	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_AUTH_CHALLENGE)
	if err != nil {
		return nil, err
	}

	var challengeMsg sessionv1.AuthChallenge
	if err := protocol.UnmarshalBody(env.Body, &challengeMsg); err != nil {
		return nil, fmt.Errorf("unmarshal auth challenge: %w", err)
	}

	payload := auth.BuildChallengePayload(protocolID, &auth.Challenge{
		ClientPeerID: clientPeerID,
		NodePeerID:   challengeMsg.NodePeerId,
		Nonce:        challengeMsg.Nonce,
		IssuedAt:     time.UnixMilli(int64(challengeMsg.IssuedAtMs)).UTC(),
		ExpiresAt:    time.UnixMilli(int64(challengeMsg.ExpiresAtMs)).UTC(),
	})
	signature, err := privKey.Sign(payload)
	if err != nil {
		return nil, fmt.Errorf("sign challenge: %w", err)
	}
	publicKey, err := corecrypto.MarshalPublicKey(privKey.GetPublic())
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}

	if err := writeEnvelope(stream, sessionv1.MsgType_AUTH_PROVE, requestID("prove"), &sessionv1.AuthProve{
		ClientPeerId: clientPeerID,
		Nonce:        challengeMsg.Nonce,
		IssuedAtMs:   challengeMsg.IssuedAtMs,
		ExpiresAtMs:  challengeMsg.ExpiresAtMs,
		Signature:    signature,
		PublicKey:    publicKey,
	}); err != nil {
		return nil, err
	}

	authEnv, err := readEnvelopeExpect(stream, sessionv1.MsgType_AUTH_RESULT)
	if err != nil {
		return nil, err
	}

	var result sessionv1.AuthResult
	if err := protocol.UnmarshalBody(authEnv.Body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal auth result: %w", err)
	}
	if !result.Ok {
		return nil, fmt.Errorf("authentication rejected")
	}
	return &result, nil
}

func printSpaces(spaces []*sessionv1.SpaceSummary) {
	fmt.Println("spaces:")
	for _, srv := range spaces {
		fmt.Printf("  - %d (%s) allow_channel_creation=%v members=%d\n", srv.SpaceId, srv.Name, srv.AllowChannelCreation, srv.MemberCount)
	}
}

func getCreateSpacePermissions(stream network.Stream) (*sessionv1.GetCreateSpacePermissionsResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_GET_CREATE_SPACE_PERMISSIONS_REQ, requestID("server-perms"), &sessionv1.GetCreateSpacePermissionsReq{}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_GET_CREATE_SPACE_PERMISSIONS_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.GetCreateSpacePermissionsResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal get create-space permissions resp: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("get create-space permissions rejected")
	}
	return &resp, nil
}

func getSpacePermissions(stream network.Stream, spaceID uint32) (*sessionv1.GetCreateGroupPermissionsResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_GET_CREATE_GROUP_PERMISSIONS_REQ, requestID("space-perms"), &sessionv1.GetCreateGroupPermissionsReq{
		SpaceId: spaceID,
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_GET_CREATE_GROUP_PERMISSIONS_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.GetCreateGroupPermissionsResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal get space permissions resp: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("get space permissions rejected")
	}
	return &resp, nil
}

func addrInfo(raw string) (*peer.AddrInfo, error) {
	maddr, err := ma.NewMultiaddr(raw)
	if err != nil {
		return nil, err
	}
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return nil, err
	}
	return info, nil
}

func parseBootstrapPeers(values bootstrapPeerFlags) ([]peer.AddrInfo, error) {
	peers := make([]peer.AddrInfo, 0, len(values)+len(dht.GetDefaultBootstrapPeerAddrInfos()))
	peers = append(peers, dht.GetDefaultBootstrapPeerAddrInfos()...)
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		info, err := addrInfo(raw)
		if err != nil {
			return nil, fmt.Errorf("parse bootstrap peer %q: %w", raw, err)
		}
		peers = append(peers, *info)
	}
	return peers, nil
}

func connectPeer(ctx context.Context, host corehost.Host, info peer.AddrInfo) error {
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return host.Connect(connectCtx, info)
}

func discoverMeshserverPeer(ctx context.Context, host corehost.Host, node *dht.IpfsDHT, namespace string) (*peer.AddrInfo, error) {
	discovery := discoveryrouting.NewRoutingDiscovery(node)
	peerChan, err := discovery.FindPeers(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("start discovery: %w", err)
	}

	for info := range peerChan {
		if info.ID == "" || info.ID == host.ID() {
			continue
		}
		if err := connectPeer(ctx, host, info); err != nil {
			log.Printf("connect discovered peer %s failed: %v", info.ID.String(), err)
			continue
		}
		return &info, nil
	}

	return nil, fmt.Errorf("no meshserver peer discovered in namespace %q", namespace)
}

func resolveMeshserverPeerByID(ctx context.Context, host corehost.Host, node *dht.IpfsDHT, nodePeerID string) (*peer.AddrInfo, error) {
	id, err := peer.Decode(strings.TrimSpace(nodePeerID))
	if err != nil {
		return nil, fmt.Errorf("decode peer id %q: %w", nodePeerID, err)
	}
	info, err := node.FindPeer(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find peer %s in dht: %w", id.String(), err)
	}
	if info.ID == "" {
		info.ID = id
	}
	if err := connectPeer(ctx, host, info); err != nil {
		return nil, fmt.Errorf("connect resolved peer %s: %w", id.String(), err)
	}
	return &info, nil
}

func requestID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func writeEnvelope(stream network.Stream, msgType sessionv1.MsgType, requestID string, body gproto.Message) error {
	return protocol.WriteEnvelope(stream, mustEnvelope(msgType, requestID, body))
}

func mustEnvelope(msgType sessionv1.MsgType, requestID string, body gproto.Message) *sessionv1.Envelope {
	env, err := protocol.NewEnvelope(msgType, requestID, body)
	if err != nil {
		panic(err)
	}
	return env
}

func readEnvelopeExpect(stream network.Stream, want sessionv1.MsgType) (*sessionv1.Envelope, error) {
	env, err := protocol.ReadEnvelope(stream)
	if err != nil {
		return nil, err
	}
	if env.MsgType != want {
		return nil, fmt.Errorf("unexpected msg type %s, want %s", env.MsgType.String(), want.String())
	}
	return env, nil
}

type bootstrapPeerFlags []string

func (f *bootstrapPeerFlags) String() string {
	return strings.Join(*f, ",")
}

func (f *bootstrapPeerFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}
