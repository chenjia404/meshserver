package media

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	offlineexchange "github.com/ipfs/boxo/exchange/offline"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"

	"meshserver/internal/config"
	"meshserver/internal/ipfsunixfs"
)

// ComputeUnixFSFileCID 以設定 `ipfs`（chunker、hash_function、raw_leaves、cid_version）對檔案位元組計算 UnixFS 根 CID，
// 與嵌入式 IPFS `/api/ipfs/add` 使用相同參數時結果一致。
func ComputeUnixFSFileCID(cfg *config.IPFSConfig, payload []byte) (string, error) {
	if len(payload) == 0 {
		return "", errors.New("file is empty")
	}
	return ComputeUnixFSFileCIDFromReader(cfg, bytes.NewReader(payload))
}

// ComputeUnixFSFileCIDFromReader 同上，自 reader 讀取全文後匯入離線 DAG。
func ComputeUnixFSFileCIDFromReader(cfg *config.IPFSConfig, r io.Reader) (string, error) {
	if cfg == nil {
		return "", errors.New("ipfs config is nil")
	}
	if r == nil {
		return "", errors.New("reader is nil")
	}
	c := *cfg
	if err := c.Normalize(); err != nil {
		return "", err
	}
	chunkSize := ipfsunixfs.ChunkSizeFromSpec(c.Chunker)
	if chunkSize <= 0 {
		chunkSize = 1048576
	}
	ds := dssync.MutexWrap(datastore.NewMapDatastore())
	bs := blockstore.NewBlockstore(ds)
	bsvc := blockservice.New(bs, offlineexchange.Exchange(bs))
	dag := merkledag.NewDAGService(bsvc)
	nd, err := ipfsunixfs.AddFileFromReader(
		context.Background(),
		dag,
		r,
		chunkSize,
		c.RawLeaves,
		c.CIDVersion,
		c.HashFunction,
	)
	if err != nil {
		return "", err
	}
	return nd.Cid().String(), nil
}
