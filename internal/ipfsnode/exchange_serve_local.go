package ipfsnode

import (
	"context"

	"github.com/ipfs/boxo/bitswap"
	blockstore "github.com/ipfs/boxo/blockstore"
	exchange "github.com/ipfs/boxo/exchange"
	offlineexchange "github.com/ipfs/boxo/exchange/offline"
	blocks "github.com/ipfs/go-block-format"
	cid "github.com/ipfs/go-cid"
)

// serveLocalExchange 供 blockservice 使用：
//   - 讀取（GetBlock / GetBlocks / NewSession）僅查本地 blockstore，不向網路拉塊；
//   - NotifyNewBlocks 轉給 bitswap，讓本節點在 bitswap 協議上向對等端提供本機已有區塊。
type serveLocalExchange struct {
	offline exchange.Interface
	bswap   *bitswap.Bitswap
}

var _ exchange.SessionExchange = (*serveLocalExchange)(nil)

func newServeLocalExchange(bs blockstore.Blockstore, bswap *bitswap.Bitswap) *serveLocalExchange {
	return &serveLocalExchange{
		offline: offlineexchange.Exchange(bs),
		bswap:   bswap,
	}
}

func (e *serveLocalExchange) GetBlock(ctx context.Context, k cid.Cid) (blocks.Block, error) {
	return e.offline.GetBlock(ctx, k)
}

func (e *serveLocalExchange) GetBlocks(ctx context.Context, ks []cid.Cid) (<-chan blocks.Block, error) {
	return e.offline.GetBlocks(ctx, ks)
}

func (e *serveLocalExchange) NotifyNewBlocks(ctx context.Context, blks ...blocks.Block) error {
	return e.bswap.NotifyNewBlocks(ctx, blks...)
}

func (e *serveLocalExchange) Close() error {
	return e.bswap.Close()
}

// NewSession 僅使用離線讀取路徑，不建立會向網路廣播 want 的 bitswap session。
func (e *serveLocalExchange) NewSession(ctx context.Context) exchange.Fetcher {
	return e.offline
}
