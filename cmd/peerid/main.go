package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p/core/peer"

	meshlibp2p "meshserver/internal/libp2p"
)

func main() {
	keyPath := flag.String("key", "docker-compose/data/config/node.key", "path to the libp2p private key")
	flag.Parse()

	privKey, err := meshlibp2p.LoadOrCreateIdentity(*keyPath)
	if err != nil {
		log.Fatalf("load key: %v", err)
	}

	peerID, err := peer.IDFromPrivateKey(privKey)
	if err != nil {
		log.Fatalf("derive peer id: %v", err)
	}

	fmt.Println(peerID.String())
}
