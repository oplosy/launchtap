-- Demo forum content for the local stack (web/scripts/local-stack.ps1). Local only: the Privy
-- DIDs are fake and the wallets are Anvil's public dev accounts. Writes users, their published
-- wallets and ~20 token comments per launched token, spread over the last two days.
BEGIN;

CREATE TEMP TABLE demo_authors (did TEXT, username TEXT, wallet BYTEA) ON COMMIT DROP;
INSERT INTO demo_authors VALUES
    ('did:privy:local-demo-0', 'deployer', decode('f39fd6e51aad88f6f4ce6ab8827279cfffb92266', 'hex')),
    ('did:privy:local-demo-1', 'curve_hunter', decode('70997970c51812dc3a010c7d01b50e0d17dc79c8', 'hex')),
    ('did:privy:local-demo-2', 'night_owl', decode('3c44cdddb6a900fa2b585dd299e03d12fa4293bc', 'hex')),
    ('did:privy:local-demo-3', 'paperhands_pat', decode('90f79bf6eb2c4f870365e785982e1f101e93b906', 'hex')),
    ('did:privy:local-demo-4', 'moonwatcher', decode('15d34aaf54267db7d7c367839aaf71a00a2c6a65', 'hex')),
    ('did:privy:local-demo-5', 'harbor_master', decode('9965507d1a55bcc2695c58ba16fb37d819b0a4dc', 'hex'));

INSERT INTO social_users (chain_id, deployment_id, privy_did, username, username_changed_at)
SELECT 31337, 'task6-anvil', did, username, now() FROM demo_authors
ON CONFLICT DO NOTHING;

INSERT INTO social_public_wallets (chain_id, deployment_id, wallet, privy_did)
SELECT 31337, 'task6-anvil', wallet, did FROM demo_authors
ON CONFLICT DO NOTHING;

CREATE TEMP TABLE demo_bodies (n INT, body TEXT) ON COMMIT DROP;
INSERT INTO demo_bodies
SELECT ordinality, body FROM unnest(ARRAY[
    'Just aped in. The curve is still early, chart looks clean.',
    'Who else is holding through graduation? Diamond hands only.',
    'Dev bought a tiny bag at launch, that is a good sign honestly.',
    'Volume picking up again in the last hour.',
    'Is there a roadmap or is this pure vibes?',
    'Bought the dip, not selling below 2x.',
    'Liquidity burned at graduation means no rug on the pool side. Love it.',
    'This ticker is going to be everywhere next week.',
    'Holders count keeps climbing, slow and steady.',
    'Took some profit, still holding a moon bag.',
    'Anyone know when the community call is?',
    'Price impact on a 0.1 ETH buy is still small, plenty of room.',
    'The name alone is worth a bag.',
    'Sold too early again. Classic me.',
    'Fixed supply, no mint function, curve math is public. This is how launches should work.',
    'GM to everyone still holding.',
    'Chart is forming a nice base. Patience.',
    'First token I have launched here, feedback welcome!',
    'Watching the graduation progress bar like it is a sports game.',
    'Added to watchlist, will buy if it holds this level.',
    'Whoever bought 0.4 ETH earlier: respect.',
    'Recent trades tab is moving fast today.',
    'Memes incoming, stay tuned.',
    'Low-key the best art on the explore page right now.'
]) WITH ORDINALITY AS t(body, ordinality);

INSERT INTO token_comments (chain_id, deployment_id, token_address, privy_did, wallet, author_role, body, created_at)
SELECT
    31337,
    'task6-anvil',
    t.token_address,
    a.did,
    a.wallet,
    CASE WHEN a.wallet = t.creator THEN 'creator' ELSE 'holder' END,
    b.body,
    now() - make_interval(mins => (random() * 2880)::int)
FROM tokens t
CROSS JOIN generate_series(1, 20) AS g(i)
JOIN LATERAL (
    SELECT * FROM demo_authors ORDER BY md5(encode(t.token_address, 'hex') || g.i::text || did) LIMIT 1
) a ON true
JOIN demo_bodies b ON b.n = 1 + (abs(hashtext(encode(t.token_address, 'hex') || g.i::text)) % 24)
WHERE t.chain_id = 31337
ORDER BY 8;

COMMIT;
