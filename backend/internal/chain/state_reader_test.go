package chain

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// The expected slot is `cast index bytes32 <poolId> 6` (and `cast keccak <poolId><6 as uint256>`),
// computed outside this code: StateLibrary._getPoolStateSlot in the pinned v4-core.
func TestV4PoolStateSlotMatchesStateLibrary(t *testing.T) {
	poolID := common.HexToHash("0x21c67e77068de97969ba93d4aab21826d33ca12bb9f565d8496e8fda8a82ca27")
	want := common.HexToHash("0xda8cac368d67cd2f2d8aaa5cc531768e0fa3b1d205c5c5de60da078e1f59bdfc")
	if got := V4PoolStateSlot(poolID); got != want {
		t.Fatalf("V4PoolStateSlot = %s, want %s", got.Hex(), want.Hex())
	}
}

func TestSqrtPriceFromSlot0TakesTheLow160Bits(t *testing.T) {
	// slot0 packs lpFee(24) | protocolFee(24) | tick(24) | sqrtPriceX96(160) from the high bits down.
	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10) // 2^96, price 1
	word := new(big.Int).Lsh(big.NewInt(3000), 208)                             // lpFee
	word.Or(word, new(big.Int).Lsh(big.NewInt(0xffffff), 160))                  // tick -1 as int24
	word.Or(word, sqrtPrice)
	if got := sqrtPriceFromSlot0(common.BigToHash(word)); got.Cmp(sqrtPrice) != 0 {
		t.Fatalf("sqrtPriceFromSlot0 = %s, want %s", got, sqrtPrice)
	}
}

func TestStateReaderPacksPoolAndPairReads(t *testing.T) {
	slot := common.HexToHash("0x01")
	data, err := stateABI.Pack("extsload", slot)
	if err != nil {
		t.Fatalf("pack extsload: %v", err)
	}
	if selector := crypto.Keccak256([]byte("extsload(bytes32)"))[:4]; !bytes.Equal(data[:4], selector) || !bytes.Equal(data[4:], slot.Bytes()) {
		t.Fatalf("extsload calldata = %x", data)
	}
	data, err = stateABI.Pack("getReserves")
	if err != nil || !bytes.Equal(data, crypto.Keccak256([]byte("getReserves()"))[:4]) {
		t.Fatalf("getReserves calldata = %x, err = %v", data, err)
	}
}
