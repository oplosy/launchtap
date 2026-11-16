-- name: LockTokenLaunch :exec
-- Holds the launch row FOR SHARE until commit through a SECURITY DEFINER function (migration
-- 00039): a row lock needs UPDATE privilege, which the API role must not have on token_launches.
SELECT lock_token_launch(sqlc.arg(chain_id)::BIGINT, sqlc.arg(token_address)::BYTEA);

-- name: GetTokenCreator :one
SELECT creator
FROM token_launches
WHERE chain_id = sqlc.arg(chain_id) AND token_address = sqlc.arg(token_address);

-- name: GetTokenImage :one
SELECT content_type, content, sha256, revision, updated_at
FROM token_images AS image
JOIN token_launches AS launch
  ON launch.chain_id = image.chain_id
 AND launch.token_address = image.token_address
 AND launch.tx_hash = image.launch_tx_hash
 AND launch.log_index = image.launch_log_index
WHERE image.chain_id = sqlc.arg(chain_id) AND image.token_address = sqlc.arg(token_address);

-- name: GetTokenMetadata :one
SELECT description, image_url, x_url, telegram_url, revision, updated_at
FROM token_metadata AS metadata
JOIN token_launches AS launch
  ON launch.chain_id = metadata.chain_id
 AND launch.token_address = metadata.token_address
 AND launch.tx_hash = metadata.launch_tx_hash
 AND launch.log_index = metadata.launch_log_index
WHERE metadata.chain_id = sqlc.arg(chain_id) AND metadata.token_address = sqlc.arg(token_address);

-- name: ReplaceTokenMetadata :one
INSERT INTO token_metadata (chain_id, token_address, launch_tx_hash, launch_log_index, description, image_url, x_url, telegram_url, revision, updated_at)
SELECT launch.chain_id, launch.token_address, launch.tx_hash, launch.log_index,
       sqlc.arg(description), sqlc.arg(image_url), sqlc.arg(x_url), sqlc.arg(telegram_url), 1, sqlc.arg(updated_at)
FROM token_launches AS launch
WHERE launch.chain_id = sqlc.arg(chain_id) AND launch.token_address = sqlc.arg(token_address)
  AND launch.creator = sqlc.arg(creator)
  AND (sqlc.arg(expected_revision)::bigint = 0 OR EXISTS (
      SELECT 1 FROM token_metadata
      WHERE chain_id = launch.chain_id AND token_address = launch.token_address
        AND launch_tx_hash = launch.tx_hash AND launch_log_index = launch.log_index
  ))
ON CONFLICT (chain_id, token_address, launch_tx_hash, launch_log_index)
WHERE launch_tx_hash IS NOT NULL AND launch_log_index IS NOT NULL DO UPDATE
SET description = EXCLUDED.description, image_url = EXCLUDED.image_url,
    x_url = EXCLUDED.x_url, telegram_url = EXCLUDED.telegram_url,
    revision = token_metadata.revision + 1, updated_at = EXCLUDED.updated_at
WHERE token_metadata.revision = sqlc.arg(expected_revision)
RETURNING revision;

-- name: ReplaceTokenImage :one
INSERT INTO token_images (chain_id, token_address, launch_tx_hash, launch_log_index, content_type, content, byte_size, sha256, revision, updated_at)
SELECT launch.chain_id, launch.token_address, launch.tx_hash, launch.log_index,
       sqlc.arg(content_type), sqlc.arg(content), sqlc.arg(byte_size), sqlc.arg(sha256), 1, sqlc.arg(updated_at)
FROM token_launches AS launch
WHERE launch.chain_id = sqlc.arg(chain_id) AND launch.token_address = sqlc.arg(token_address)
  AND launch.creator = sqlc.arg(creator)
  AND (sqlc.arg(expected_revision)::bigint = 0 OR EXISTS (
      SELECT 1 FROM token_images
      WHERE chain_id = launch.chain_id AND token_address = launch.token_address
        AND launch_tx_hash = launch.tx_hash AND launch_log_index = launch.log_index
  ))
ON CONFLICT (chain_id, token_address, launch_tx_hash, launch_log_index)
WHERE launch_tx_hash IS NOT NULL AND launch_log_index IS NOT NULL DO UPDATE
SET content_type = EXCLUDED.content_type, content = EXCLUDED.content,
    byte_size = EXCLUDED.byte_size, sha256 = EXCLUDED.sha256,
    revision = token_images.revision + 1, updated_at = EXCLUDED.updated_at
WHERE token_images.revision = sqlc.arg(expected_revision)
RETURNING revision;
