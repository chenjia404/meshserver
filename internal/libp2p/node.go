package libp2p

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	corecrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	p2pprotocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/routing"
	discoveryrouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2ptls "github.com/libp2p/go-libp2p/p2p/security/tls"
	"github.com/multiformats/go-multiaddr"

	"meshserver/internal/config"
)

// Node is the local libp2p host wrapper.
type Node struct {
	cfg            *config.Config
	logger         *slog.Logger
	host           host.Host
	routing        routing.Routing
	dht            *dht.IpfsDHT
	bootstrapPeers []peer.AddrInfo
	discoveryNS    string
	sessionHandler network.StreamHandler
}

// NewNode creates a libp2p host and prepares the session protocol handler.
func NewNode(ctx context.Context, cfg *config.Config, logger *slog.Logger, sessionHandler network.StreamHandler) (*Node, error) {
	privKey, err := LoadOrCreateIdentity(cfg.NodeKeyPath)
	if err != nil {
		return nil, err
	}

	// connmgr_, _ := connmgr.NewConnManager(
	// 	50,
	// 	400,
	// 	connmgr.WithGracePeriod(time.Minute),
	// )

	bootstrapPeers, err := collectDHTBootstrapPeers(cfg.DHTBootstrapPeers)
	if err != nil {
		return nil, err
	}

	var nodeRouting routing.Routing
	var nodeDHT *dht.IpfsDHT

	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(cfg.Libp2pListenAddrs...),
		libp2p.DefaultTransports,
		libp2p.Security(libp2ptls.ID, libp2ptls.New),
		libp2p.Security(noise.ID, noise.New),
		libp2p.DefaultPeerstore,
		// libp2p.ConnectionManager(connmgr_),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			instance, err := dht.New(ctx, h, dht.Mode(dht.ModeAuto), dht.BootstrapPeers(bootstrapPeers...))
			if err == nil {
				nodeRouting = instance
				nodeDHT = instance
			}
			return instance, err
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}

	return &Node{
		cfg:            cfg,
		logger:         logger,
		host:           h,
		routing:        nodeRouting,
		dht:            nodeDHT,
		bootstrapPeers: bootstrapPeers,
		discoveryNS:    cfg.DHTDiscoveryNamespace,
		sessionHandler: sessionHandler,
	}, nil
}

// Start registers the session protocol handler and logs the listen addresses.
func (n *Node) Start(ctx context.Context) error {
	if n.sessionHandler != nil {
		n.host.SetStreamHandler(p2pprotocol.ID(n.cfg.Libp2pProtocolID), n.sessionHandler)
	}
	if len(n.bootstrapPeers) > 0 {
		n.connectBootstrapPeers(ctx)
		go n.connectBootstrapPeersLoop(ctx)
	}
	if n.dht != nil {
		if err := n.dht.Bootstrap(ctx); err != nil {
			return fmt.Errorf("bootstrap dht: %w", err)
		}
	}
	if n.dht != nil && strings.TrimSpace(n.discoveryNS) != "" {
		go n.advertiseDiscoveryLoop(ctx)
	}
	addrs := make([]string, 0, len(n.host.Addrs()))
	for _, addr := range n.host.Addrs() {
		addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", addr.String(), n.host.ID().String()))
	}
	n.logger.Info("libp2p node started", "peer_id", n.host.ID().String(), "listen_addrs", addrs)
	return nil
}

// Close closes the underlying host.
func (n *Node) Close() error {
	var errs []error
	if n.dht != nil {
		if err := n.dht.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close dht: %w", err))
		}
	}
	if err := n.host.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close host: %w", err))
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// SetSessionHandler replaces the active session handler and registers it immediately.
func (n *Node) SetSessionHandler(handler network.StreamHandler) {
	n.sessionHandler = handler
	n.host.SetStreamHandler(p2pprotocol.ID(n.cfg.Libp2pProtocolID), handler)
}

// Host returns the underlying libp2p host.
func (n *Node) Host() host.Host {
	return n.host
}

// Routing returns the node routing table, if initialized.
func (n *Node) Routing() routing.Routing {
	return n.routing
}

// DHT returns the underlying Kademlia DHT, if initialized.
func (n *Node) DHT() *dht.IpfsDHT {
	return n.dht
}

func (n *Node) connectBootstrapPeersLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		if n.connectBootstrapPeers(ctx) {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (n *Node) connectBootstrapPeers(ctx context.Context) bool {
	connected := false
	for _, info := range n.bootstrapPeers {
		if n.host.Network().Connectedness(info.ID) == network.Connected {
			connected = true
			continue
		}

		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := n.host.Connect(connectCtx, info)
		cancel()
		if err != nil {
			n.logger.Warn("connect bootstrap peer failed", "peer_id", info.ID.String(), "err", err)
			continue
		}

		connected = true
		n.logger.Info("connected bootstrap peer", "peer_id", info.ID.String(), "addrs", info.Addrs)
	}
	return connected
}

func (n *Node) advertiseDiscoveryLoop(ctx context.Context) {
	discovery := discoveryrouting.NewRoutingDiscovery(n.dht)
	refreshEvery := 30 * time.Minute

	for {
		ttl, err := discovery.Advertise(ctx, n.discoveryNS)
		if err != nil {
			n.logger.Warn("advertise discovery namespace failed", "namespace", n.discoveryNS, "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
			continue
		}

		wait := ttl / 2
		if wait <= 0 {
			wait = refreshEvery
		}
		if wait > refreshEvery {
			wait = refreshEvery
		}
		n.logger.Info("advertised discovery namespace", "namespace", n.discoveryNS, "ttl", ttl)

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// PeerID returns the host peer ID.
func (n *Node) PeerID() string {
	return n.host.ID().String()
}

// PublicAddrs returns the announced listen addresses with peer ID suffixes.
func (n *Node) PublicAddrs() []string {
	out := make([]string, 0, len(n.host.Addrs()))
	for _, addr := range n.host.Addrs() {
		out = append(out, fmt.Sprintf("%s/p2p/%s", addr.String(), n.host.ID().String()))
	}
	return out
}

// LoadOrCreateIdentity loads a persisted libp2p private key or creates one.
func LoadOrCreateIdentity(path string) (corecrypto.PrivKey, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create node key dir: %w", err)
	}

	if raw, err := os.ReadFile(path); err == nil {
		privKey, err := corecrypto.UnmarshalPrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("unmarshal node key: %w", err)
		}
		return privKey, nil
	}

	privKey, _, err := corecrypto.GenerateEd25519Key(nil)
	if err != nil {
		return nil, fmt.Errorf("generate node key: %w", err)
	}
	raw, err := corecrypto.MarshalPrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("marshal node key: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, fmt.Errorf("persist node key: %w", err)
	}
	return privKey, nil
}

func collectDHTBootstrapPeers(extra []string) ([]peer.AddrInfo, error) {
	peers := append([]peer.AddrInfo{}, dht.GetDefaultBootstrapPeerAddrInfos()...)
	for _, addrStr := range extra {
		addrStr = strings.TrimSpace(addrStr)
		if addrStr == "" {
			continue
		}
		addr, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			return nil, fmt.Errorf("invalid dht bootstrap multiaddr %q: %w", addrStr, err)
		}
		info, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			return nil, fmt.Errorf("parse dht bootstrap peer %q: %w", addrStr, err)
		}
		peers = append(peers, *info)
	}
	return peers, nil
}
