//go:build integration

package postgrestest

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/metadata"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
)

func TestMetadataAndImagesAuthorizeAndUseRevisionsAtomically(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := storepostgres.OpenPool(ctx, database.URLAs(t, APIRole), storepostgres.PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	const chainID int64 = 46630
	at := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	blockHash, tokenBytes := hashBytes(0x81), addressBytes(0x82)
	launchTxHash := hashBytes(0x83)
	mustInsertBlock(t, ctx, database.DB, chainID, 10, blockHash, hashBytes(0x80), at, "safe")
	insertProjectionLaunch(t, ctx, database.DB, chainID, 10, blockHash, at, launchTxHash, projectionLaunchFixture{token: tokenBytes, curve: addressBytes(0x84), pair: addressBytes(0x85), weth: addressBytes(0x86)})
	if _, err := database.DB.ExecContext(ctx, `SELECT rebuild_token_projections($1,$2)`, chainID, tokenBytes); err != nil {
		t.Fatal(err)
	}
	store := storepostgres.MetadataStore{Pool: pool, DeploymentID: "metadata-test"}
	tokenAddress := common.BytesToAddress(tokenBytes)
	creator := common.BytesToAddress(addressBytes(0x55))

	if _, err := store.ReplaceMetadata(ctx, chainID, tokenAddress, []common.Address{common.HexToAddress("0x01")}, metadata.Metadata{Revision: 0, UpdatedAt: at}); !errors.Is(err, metadata.ErrUnauthorized) {
		t.Fatalf("unauthorized error=%v", err)
	}
	revision, err := store.ReplaceMetadata(ctx, chainID, tokenAddress, []common.Address{creator}, metadata.Metadata{Description: "first", Revision: 0, UpdatedAt: at})
	if err != nil || revision != 1 {
		t.Fatalf("create metadata revision=%d error=%v", revision, err)
	}
	revision, err = store.ReplaceMetadata(ctx, chainID, tokenAddress, []common.Address{creator}, metadata.Metadata{Description: "second", Revision: 1, UpdatedAt: at.Add(time.Second)})
	if err != nil || revision != 2 {
		t.Fatalf("replace metadata revision=%d error=%v", revision, err)
	}
	if _, err := store.ReplaceMetadata(ctx, chainID, tokenAddress, []common.Address{creator}, metadata.Metadata{Revision: 0, UpdatedAt: at}); !errors.Is(err, metadata.ErrRevisionConflict) {
		t.Fatalf("stale metadata error=%v", err)
	}

	content := []byte("\x89PNG\r\n\x1a\nfixture")
	hash := sha256.Sum256(content)
	revision, err = store.ReplaceImage(ctx, chainID, tokenAddress, []common.Address{creator}, metadata.Image{ContentType: "image/png", Content: content, SHA256: hash, Revision: 0, UpdatedAt: at})
	if err != nil || revision != 1 {
		t.Fatalf("create image revision=%d error=%v", revision, err)
	}
	image, err := store.GetImage(ctx, chainID, tokenAddress)
	if err != nil || image.SHA256 != hash || image.ContentType != "image/png" {
		t.Fatalf("image=%+v error=%v", image, err)
	}

	removeLaunch := func() {
		t.Helper()
		for _, table := range []string{"aggregation_dirty", "token_reserves", "holder_balances", "candles", "token_stats", "tokens"} {
			if _, err := database.DB.ExecContext(ctx, `DELETE FROM `+table+` WHERE chain_id=$1 AND token_address=$2`, chainID, tokenBytes); err != nil {
				t.Fatalf("delete %s projection with image: %v", table, err)
			}
		}
		if _, err := database.DB.ExecContext(ctx, `DELETE FROM token_launches WHERE chain_id=$1 AND token_address=$2`, chainID, tokenBytes); err != nil {
			t.Fatalf("delete launch with image: %v", err)
		}
	}
	removeLaunch()
	if _, err := store.GetImage(ctx, chainID, tokenAddress); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("orphan image read error=%v, want not found", err)
	}
	if _, err := store.GetMetadata(ctx, chainID, tokenAddress); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("orphan metadata read error=%v, want not found", err)
	}

	// A replacement launch at the same address must not inherit the previous
	// launch's creator-controlled metadata or image.
	replacementTxHash := hashBytes(0x87)
	insertProjectionLaunch(t, ctx, database.DB, chainID, 10, blockHash, at, replacementTxHash, projectionLaunchFixture{token: tokenBytes, curve: addressBytes(0x88), pair: addressBytes(0x89), weth: addressBytes(0x8a)})
	if _, err := database.DB.ExecContext(ctx, `UPDATE token_launches SET creator=$3 WHERE chain_id=$1 AND tx_hash=$2`, chainID, replacementTxHash, addressBytes(0x66)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.ExecContext(ctx, `SELECT rebuild_token_projections($1,$2)`, chainID, tokenBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetImage(ctx, chainID, tokenAddress); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("replacement launch image read error=%v, want not found", err)
	}
	if _, err := store.GetMetadata(ctx, chainID, tokenAddress); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("replacement launch metadata read error=%v, want not found", err)
	}
	detail, err := sqlc.New(pool).GetTokenDetail(ctx, sqlc.GetTokenDetailParams{ChainID: chainID, TokenAddress: sqlc.Address(tokenAddress)})
	if err != nil || detail.Description.Valid || detail.ImageUrl.Valid || detail.XUrl.Valid || detail.TelegramUrl.Valid {
		t.Fatalf("replacement launch detail leaked prior metadata: detail=%+v error=%v", detail, err)
	}
	if _, err := store.ReplaceImage(ctx, chainID, tokenAddress, []common.Address{creator}, metadata.Image{ContentType: "image/png", Content: content, SHA256: hash, Revision: 0, UpdatedAt: at}); !errors.Is(err, metadata.ErrUnauthorized) {
		t.Fatalf("prior creator write against replacement launch error=%v, want unauthorized", err)
	}

	// Replaying the original canonical launch identity makes its own content
	// visible again, which is the intended reorg-survival behavior.
	removeLaunch()
	insertProjectionLaunch(t, ctx, database.DB, chainID, 10, blockHash, at, launchTxHash, projectionLaunchFixture{token: tokenBytes, curve: addressBytes(0x84), pair: addressBytes(0x85), weth: addressBytes(0x86)})
	if _, err := database.DB.ExecContext(ctx, `SELECT rebuild_token_projections($1,$2)`, chainID, tokenBytes); err != nil {
		t.Fatal(err)
	}
	image, err = store.GetImage(ctx, chainID, tokenAddress)
	if err != nil || image.SHA256 != hash || image.ContentType != "image/png" {
		t.Fatalf("same-launch replay image=%+v error=%v", image, err)
	}
	metadataValue, err := store.GetMetadata(ctx, chainID, tokenAddress)
	if err != nil || metadataValue.Description != "second" {
		t.Fatalf("same-launch replay metadata=%+v error=%v", metadataValue, err)
	}
	detail, err = sqlc.New(pool).GetTokenDetail(ctx, sqlc.GetTokenDetailParams{ChainID: chainID, TokenAddress: sqlc.Address(tokenAddress)})
	if err != nil || !detail.Description.Valid || detail.Description.String != "second" {
		t.Fatalf("same-launch replay detail=%+v error=%v", detail, err)
	}
	if _, err := store.ReplaceImage(ctx, chainID, tokenAddress, []common.Address{creator}, metadata.Image{ContentType: "image/png", Content: content, SHA256: hash, Revision: 0, UpdatedAt: at}); !errors.Is(err, metadata.ErrRevisionConflict) {
		t.Fatalf("stale image write error=%v", err)
	}
}

func TestMetadataAndImageFirstWritesRejectConcurrentExpectedZero(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := storepostgres.OpenPool(ctx, database.URLAs(t, APIRole), storepostgres.PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	const chainID int64 = 46630
	at := time.Date(2026, 9, 9, 13, 0, 0, 0, time.UTC)
	creator := common.Address(addressBytes(0x55))
	metadataToken := addressBytes(0x92)
	imageToken := addressBytes(0x93)
	for block, tokenBytes := range map[int64][]byte{11: metadataToken, 12: imageToken} {
		blockHash := hashBytes(byte(block))
		mustInsertBlock(t, ctx, database.DB, chainID, block, blockHash, hashBytes(byte(block-1)), at, "safe")
		insertProjectionLaunch(t, ctx, database.DB, chainID, block, blockHash, at, hashBytes(byte(block+20)), projectionLaunchFixture{token: tokenBytes, curve: addressBytes(byte(block + 30)), pair: addressBytes(byte(block + 40)), weth: addressBytes(byte(block + 50))})
		if _, err := database.DB.ExecContext(ctx, `SELECT rebuild_token_projections($1,$2)`, chainID, tokenBytes); err != nil {
			t.Fatal(err)
		}
	}
	store := storepostgres.MetadataStore{Pool: pool, DeploymentID: "metadata-concurrency-test"}
	content := []byte("\x89PNG\r\n\x1a\nconcurrent")
	hash := sha256.Sum256(content)

	metadataRevisions := make(chan int64, 2)
	metadataErrors := make(chan error, 2)
	imageRevisions := make(chan int64, 2)
	imageErrors := make(chan error, 2)
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			<-start
			revision, err := store.ReplaceMetadata(ctx, chainID, common.BytesToAddress(metadataToken), []common.Address{creator}, metadata.Metadata{Description: string(rune('A' + i)), Revision: 0, UpdatedAt: at.Add(time.Duration(i) * time.Second)})
			metadataRevisions <- revision
			metadataErrors <- err
		}(i)
	}
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			<-start
			revision, err := store.ReplaceImage(ctx, chainID, common.BytesToAddress(imageToken), []common.Address{creator}, metadata.Image{ContentType: "image/png", Content: content, SHA256: hash, Revision: 0, UpdatedAt: at.Add(time.Duration(i) * time.Second)})
			imageRevisions <- revision
			imageErrors <- err
		}(i)
	}
	close(start)
	group.Wait()
	close(metadataRevisions)
	close(metadataErrors)
	close(imageRevisions)
	close(imageErrors)

	metadataSuccess, metadataConflict := 0, 0
	for revision := range metadataRevisions {
		if revision == 1 {
			metadataSuccess++
		}
	}
	for err := range metadataErrors {
		if err == nil {
			continue
		}
		if errors.Is(err, metadata.ErrRevisionConflict) {
			metadataConflict++
		} else {
			t.Errorf("metadata concurrent write error=%v", err)
		}
	}
	if metadataSuccess != 1 || metadataConflict != 1 {
		t.Fatalf("metadata concurrent result success=%d conflict=%d", metadataSuccess, metadataConflict)
	}
	imageSuccess, imageConflict := 0, 0
	for revision := range imageRevisions {
		if revision == 1 {
			imageSuccess++
		}
	}
	for err := range imageErrors {
		if err == nil {
			continue
		}
		if errors.Is(err, metadata.ErrRevisionConflict) {
			imageConflict++
		} else {
			t.Errorf("image concurrent write error=%v", err)
		}
	}
	if imageSuccess != 1 || imageConflict != 1 {
		t.Fatalf("image concurrent result success=%d conflict=%d", imageSuccess, imageConflict)
	}
}
