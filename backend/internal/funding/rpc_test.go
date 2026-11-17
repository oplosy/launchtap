package funding

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// A CALLCODE frame's value never reaches To, so it must not be recorded as a transfer that could
// name a forged funder (security-audit run-4).
func TestAppendFrameSkipsValuelessContextFrames(t *testing.T) {
	eoa, z, y, wallet := common.HexToAddress("0xa"), common.HexToAddress("0xb"), common.HexToAddress("0xc"), common.HexToAddress("0xd")
	value := func(v int64) *hexutil.Big { return (*hexutil.Big)(big.NewInt(v)) }
	for _, frameType := range []string{"CALLCODE", "DELEGATECALL", "STATICCALL"} {
		root := callFrame{Type: "CALL", From: eoa, To: &z, Value: value(0), Calls: []callFrame{
			{Type: "CALL", From: z, To: &y, Value: value(0), Calls: []callFrame{
				{Type: frameType, From: y, To: &wallet, Value: value(1)},
			}},
			{Type: "CALL", From: z, To: &wallet, Value: value(2)},
		}}
		got := appendFrame(nil, common.Hash{}, root)
		if len(got) != 1 || got[0].From != z || got[0].To != wallet || got[0].Value.Int64() != 2 {
			t.Fatalf("%s: transfers = %+v, want only Z -> wallet value 2", frameType, got)
		}
	}
}
