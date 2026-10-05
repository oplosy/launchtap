-- +goose Up
-- Market-cap and 24h-volume token lists sorted a LEFT JOIN of token_stats with a COALESCE,
-- which no index can serve, so every page sorted all tokens of a phase. These columns mirror
-- token_stats (0 when no stats row exists, matching the old COALESCE) and are maintained by
-- triggers in both directions, so the list can walk a (phase, metric, address) index.
ALTER TABLE tokens
    ADD COLUMN sort_market_cap_eth_wad NUMERIC(78,0) NOT NULL DEFAULT 0,
    ADD COLUMN sort_volume_24h_eth_wad NUMERIC(78,0) NOT NULL DEFAULT 0;

UPDATE tokens AS t
SET sort_market_cap_eth_wad = s.market_cap_eth_wad,
    sort_volume_24h_eth_wad = s.volume_24h_eth_wad
FROM token_stats AS s
WHERE s.chain_id = t.chain_id AND s.token_address = t.token_address;

-- +goose StatementBegin
CREATE FUNCTION sync_token_sort_metrics_from_stats()
RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE public.tokens
        SET sort_market_cap_eth_wad = 0, sort_volume_24h_eth_wad = 0
        WHERE chain_id = OLD.chain_id AND token_address = OLD.token_address;
        RETURN OLD;
    END IF;
    UPDATE public.tokens
    SET sort_market_cap_eth_wad = NEW.market_cap_eth_wad,
        sort_volume_24h_eth_wad = NEW.volume_24h_eth_wad
    WHERE chain_id = NEW.chain_id AND token_address = NEW.token_address
      AND (sort_market_cap_eth_wad, sort_volume_24h_eth_wad)
          IS DISTINCT FROM (NEW.market_cap_eth_wad, NEW.volume_24h_eth_wad);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION load_token_sort_metrics_on_insert()
RETURNS trigger AS $$
BEGIN
    -- A stats row written before its token row (the FK is deferred) must not be lost.
    SELECT stats.market_cap_eth_wad, stats.volume_24h_eth_wad
    INTO NEW.sort_market_cap_eth_wad, NEW.sort_volume_24h_eth_wad
    FROM public.token_stats AS stats
    WHERE stats.chain_id = NEW.chain_id AND stats.token_address = NEW.token_address;
    IF NOT FOUND THEN
        NEW.sort_market_cap_eth_wad := 0;
        NEW.sort_volume_24h_eth_wad := 0;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER token_stats_sync_token_sort_metrics
    AFTER INSERT OR DELETE OR UPDATE OF market_cap_eth_wad, volume_24h_eth_wad ON token_stats
    FOR EACH ROW EXECUTE FUNCTION sync_token_sort_metrics_from_stats();
CREATE TRIGGER tokens_load_sort_metrics
    BEFORE INSERT ON tokens
    FOR EACH ROW EXECUTE FUNCTION load_token_sort_metrics_on_insert();

CREATE INDEX tokens_phase_market_cap_cursor_idx
    ON tokens (chain_id, phase, sort_market_cap_eth_wad DESC, token_address DESC);
CREATE INDEX tokens_phase_volume_cursor_idx
    ON tokens (chain_id, phase, sort_volume_24h_eth_wad DESC, token_address DESC);
DROP INDEX token_stats_market_cap_cursor_idx;
DROP INDEX token_stats_volume_cursor_idx;

-- +goose Down
CREATE INDEX token_stats_market_cap_cursor_idx
    ON token_stats (chain_id, market_cap_eth_wad DESC, token_address DESC);
CREATE INDEX token_stats_volume_cursor_idx
    ON token_stats (chain_id, volume_24h_eth_wad DESC, token_address DESC);
DROP INDEX tokens_phase_volume_cursor_idx;
DROP INDEX tokens_phase_market_cap_cursor_idx;
DROP TRIGGER tokens_load_sort_metrics ON tokens;
DROP TRIGGER token_stats_sync_token_sort_metrics ON token_stats;
DROP FUNCTION load_token_sort_metrics_on_insert();
DROP FUNCTION sync_token_sort_metrics_from_stats();
ALTER TABLE tokens
    DROP COLUMN sort_volume_24h_eth_wad,
    DROP COLUMN sort_market_cap_eth_wad;
