// Pure helpers behind scripts/generate-contracts.mjs, split out so the reviewed-deployment
// rules are unit tested (Plan v2-C Task 1).

/** Keeps the allowlisted functions, events and errors of an ABI document. */
export function filterAbi(document, names, { allErrors = false } = {}) {
  const entries = document.abi ?? document;
  return entries.filter(
    (entry) =>
      (entry.type === "function" || entry.type === "error" || entry.type === "event") &&
      (names.has(entry.name) || (allErrors && entry.type === "error")),
  );
}

const sameAddress = (left, right) =>
  typeof left === "string" &&
  typeof right === "string" &&
  left.toLowerCase() === right.toLowerCase();

/**
 * Builds one browser-facing deployment entry. Engine-2 entries join the reviewed v2 manifest
 * (factory, hook, locker) with the reviewed V4 dependency record of the same chain
 * (PoolManager, Permit2, UniversalRouter, V4Quoter); the two must agree on the shared
 * addresses, so the browser never signs against a mixed pair.
 */
export function deploymentEntry({ document, enabled, v4Record = null }) {
  const engineVersion = document.engineVersion ?? 1;
  if (engineVersion !== 1 && engineVersion !== 2)
    throw new Error(`${document.deploymentId}: unsupported engine ${engineVersion}`);
  const base = {
    deploymentId: document.deploymentId ?? document.target,
    chainId: document.chainId,
    name: document.name ?? document.target,
    engineVersion,
    enabled: enabled && Boolean(document.factory),
    factory: document.factory ?? null,
    explorerBase: document.explorerBase ?? null,
    marketLinks: document.marketLinks ?? null,
    weth: null,
    uniswapV2Factory: null,
    uniswapV2Router02: null,
    v4: null,
  };
  if (engineVersion === 1) {
    return {
      ...base,
      weth: document.weth ?? null,
      uniswapV2Factory: document.uniswapV2Factory ?? null,
      uniswapV2Router02: document.uniswapV2Router02 ?? null,
    };
  }
  if (!v4Record)
    throw new Error(`${base.deploymentId}: no reviewed V4 record for chain ${base.chainId}`);
  if (v4Record.chainId !== document.chainId)
    throw new Error(`${base.deploymentId}: V4 record is for chain ${v4Record.chainId}`);
  for (const name of ["poolManager", "permit2"]) {
    if (!sameAddress(document[name], v4Record[name]))
      throw new Error(`${base.deploymentId}: ${name} differs from the reviewed V4 record`);
  }
  return {
    ...base,
    v4: {
      poolManager: document.poolManager,
      permit2: document.permit2,
      universalRouter: v4Record.universalRouter ?? null,
      v4Quoter: v4Record.v4Quoter ?? null,
      hook: document.hook ?? null,
      locker: document.locker ?? null,
    },
  };
}
