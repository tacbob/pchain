package core_grpc

import (
	core "github.com/ethereum/go-ethereum/consensus/tendermint/rpc/core"

	context "golang.org/x/net/context"
)

type broadcastAPI struct {
}

func (bapi *broadcastAPI) BroadcastTx(ctx context.Context, req *RequestBroadcastTx) (*ResponseBroadcastTx, error) {
	res, err := core.BroadcastTxCommit(nil, req.Tx)
	if err != nil {
		return nil, err
	}
	return &ResponseBroadcastTx{res.CheckTx, res.DeliverTx}, nil
}