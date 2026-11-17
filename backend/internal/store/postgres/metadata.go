package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Contictus/launchtap/backend/internal/metadata"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrWriteUnauthorized = metadata.ErrUnauthorized
	ErrRevisionConflict  = metadata.ErrRevisionConflict
)

type MetadataStore struct {
	Pool         *pgxpool.Pool
	DeploymentID string
}

func (store MetadataStore) ReplaceMetadata(ctx context.Context, chainID int64, token common.Address, wallets []common.Address, value metadata.Metadata) (int64, error) {
	var revision int64
	err := WithinTx(ctx, store.Pool, func(ctx context.Context, adapter *Adapter) error {
		creator, err := adapter.authorizedCreator(ctx, chainID, token, wallets)
		if err != nil {
			return err
		}
		revision, err = adapter.ReplaceTokenMetadata(ctx, chainID, token, creator, value.Description, value.ImageURL, value.XURL, value.TelegramURL, value.Revision, value.UpdatedAt)
		return err
	})
	if err != nil {
		return 0, err
	}
	store.notifyToken(ctx, chainID, token)
	return revision, nil
}

func (store MetadataStore) ReplaceImage(ctx context.Context, chainID int64, token common.Address, wallets []common.Address, value metadata.Image) (int64, error) {
	var revision int64
	err := WithinTx(ctx, store.Pool, func(ctx context.Context, adapter *Adapter) error {
		creator, err := adapter.authorizedCreator(ctx, chainID, token, wallets)
		if err != nil {
			return err
		}
		revision, err = adapter.ReplaceTokenImage(ctx, chainID, token, creator, value.ContentType, value.Content, value.SHA256[:], value.Revision, value.UpdatedAt)
		return err
	})
	if err != nil {
		return 0, err
	}
	store.notifyToken(ctx, chainID, token)
	return revision, nil
}

func (store MetadataStore) GetImage(ctx context.Context, chainID int64, token common.Address) (metadata.Image, error) {
	row, err := NewAdapter(store.Pool).queries.GetTokenImage(ctx, sqlc.GetTokenImageParams{ChainID: chainID, TokenAddress: sqlc.Address(token)})
	if errors.Is(err, pgx.ErrNoRows) {
		return metadata.Image{}, metadata.ErrNotFound
	}
	if err != nil {
		return metadata.Image{}, fmt.Errorf("get token image: %w", err)
	}
	if !row.UpdatedAt.Valid {
		return metadata.Image{}, errors.New("token image has invalid updated_at")
	}
	return metadata.Image{ContentType: row.ContentType, Content: append([]byte(nil), row.Content...), SHA256: [32]byte(row.Sha256), Revision: row.Revision, UpdatedAt: row.UpdatedAt.Time}, nil
}

func (store MetadataStore) GetMetadata(ctx context.Context, chainID int64, token common.Address) (metadata.Metadata, error) {
	row, err := NewAdapter(store.Pool).queries.GetTokenMetadata(ctx, sqlc.GetTokenMetadataParams{ChainID: chainID, TokenAddress: sqlc.Address(token)})
	if errors.Is(err, pgx.ErrNoRows) {
		return metadata.Metadata{}, metadata.ErrNotFound
	}
	if err != nil {
		return metadata.Metadata{}, fmt.Errorf("get token metadata: %w", err)
	}
	if !row.UpdatedAt.Valid {
		return metadata.Metadata{}, errors.New("token metadata has invalid updated_at")
	}
	return metadata.Metadata{Description: row.Description.String, ImageURL: row.ImageUrl.String, XURL: row.XUrl.String, TelegramURL: row.TelegramUrl.String, Revision: row.Revision, UpdatedAt: row.UpdatedAt.Time}, nil
}

func (adapter *Adapter) authorizedCreator(ctx context.Context, chainID int64, token common.Address, wallets []common.Address) (common.Address, error) {
	// The lock keeps the launch (and so its creator) until this write commits, as a reorg could
	// otherwise delete it between the check and the write.
	if err := adapter.queries.LockTokenLaunch(ctx, sqlc.LockTokenLaunchParams{ChainID: chainID, TokenAddress: token.Bytes()}); err != nil {
		return common.Address{}, fmt.Errorf("lock token launch: %w", err)
	}
	creator, err := adapter.queries.GetTokenCreator(ctx, sqlc.GetTokenCreatorParams{ChainID: chainID, TokenAddress: sqlc.Address(token)})
	if errors.Is(err, pgx.ErrNoRows) {
		return common.Address{}, metadata.ErrNotFound
	}
	if err != nil {
		return common.Address{}, fmt.Errorf("read token creator: %w", err)
	}
	want := common.Address(creator)
	for _, wallet := range wallets {
		if wallet == want {
			return want, nil
		}
	}
	return common.Address{}, metadata.ErrUnauthorized
}

func (store MetadataStore) notifyToken(ctx context.Context, chainID int64, token common.Address) {
	payload, _ := json.Marshal(map[string]any{"type": "token", "chain_id": chainID, "deployment_id": store.DeploymentID, "token": token.Hex()})
	if _, err := store.Pool.Exec(ctx, `SELECT pg_notify('api_refresh', $1)`, string(payload)); err != nil {
		slog.Warn("API refresh notification failed", "error", err)
	}
}

func (adapter *Adapter) ReplaceTokenMetadata(ctx context.Context, chainID int64, token, creator common.Address, description, imageURL, xURL, telegramURL string, expectedRevision int64, now time.Time) (int64, error) {
	revision, err := adapter.queries.ReplaceTokenMetadata(ctx, sqlc.ReplaceTokenMetadataParams{
		ChainID: chainID, TokenAddress: sqlc.Address(token), Creator: sqlc.Address(creator),
		Description: pgtype.Text{String: description, Valid: description != ""}, ImageUrl: pgtype.Text{String: imageURL, Valid: imageURL != ""},
		XUrl: pgtype.Text{String: xURL, Valid: xURL != ""}, TelegramUrl: pgtype.Text{String: telegramURL, Valid: telegramURL != ""},
		ExpectedRevision: expectedRevision, UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("%w: metadata", ErrRevisionConflict)
	}
	if err != nil {
		return 0, fmt.Errorf("replace token metadata: %w", err)
	}
	return revision, nil
}

func (adapter *Adapter) ReplaceTokenImage(ctx context.Context, chainID int64, token, creator common.Address, contentType string, content, sha256 []byte, expectedRevision int64, now time.Time) (int64, error) {
	if len(sha256) != 32 {
		return 0, fmt.Errorf("image sha256 must be 32 bytes")
	}
	revision, err := adapter.queries.ReplaceTokenImage(ctx, sqlc.ReplaceTokenImageParams{
		ChainID: chainID, TokenAddress: sqlc.Address(token), Creator: sqlc.Address(creator), ContentType: contentType,
		Content: content, ByteSize: int32(len(content)), Sha256: sqlc.Hash(sha256), ExpectedRevision: expectedRevision,
		UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("%w: image", ErrRevisionConflict)
	}
	if err != nil {
		return 0, fmt.Errorf("replace token image: %w", err)
	}
	return revision, nil
}
