/**
 * Every user-facing error sentence, in one place (docs/specs/2026-11-16-user-error-messages-preflight.md).
 * Keys are stable codes: API problem codes, contract error names, Uniswap revert reasons,
 * Solidity panic codes, and wallet/RPC/network failure kinds. `errors/catalog.test.ts` fails CI
 * when a code the API or a reviewed contract can return has no entry here.
 */

/** API problem codes (the last segment of the problem `type`). */
export const apiMessages: Readonly<Record<string, string>> = {
  // Authentication and accounts
  authentication_failed: "Sign in again to continue.",
  forbidden: "The signed-in account is not allowed to do this.",
  banned: "This account is restricted and cannot post, vote or report.",
  wallet_not_linked:
    "That wallet is not linked to your account. Link it in your account settings, then try again.",
  creator_required:
    "Only the wallet that launched this token can change its details. Sign in with that wallet.",
  // Tokens, trades and transactions
  token_not_found:
    "This token was not found. Check the address, or wait a moment if it was just launched.",
  wrong_phase: "This token has left the curve. Refresh the page to trade it on the pool.",
  oversell: "You cannot sell more than the curve has sold. Lower the amount.",
  zero_input: "Enter an amount greater than zero.",
  zero_output: "This amount is too small to receive anything. Increase it.",
  quote_failed: "The price quote could not be calculated. Try again in a moment.",
  invalid_amount: "Enter a valid amount.",
  invalid_side: "Choose buy or sell.",
  invalid_address: "That is not a valid address.",
  invalid_transaction_hash: "That transaction hash is not valid.",
  transaction_not_indexed: "This transaction is not indexed yet. It will appear shortly.",
  snapshot_unavailable: "The indexer has not caught up yet. Try again in a few seconds.",
  // Lists, filters and pages
  cursor_invalidated: "This list changed while you were reading it. Reload it to continue.",
  invalid_cursor: "This page marker is no longer valid. Reload the list.",
  cursor_mode_mismatch: "This page marker belongs to another view. Reload the list.",
  cursor_not_supported: "Open positions load in one page. Reload the list.",
  invalid_limit: "The list asked for too many items at once. Reload the page.",
  invalid_sort: "That sort order is not supported. Pick another one.",
  invalid_phase: "That token phase filter is not supported. Reset the filters.",
  invalid_age: "That age filter is not supported. Reset the filters.",
  invalid_window: "That time window is not supported. Pick another one.",
  invalid_query: "Search text can be at most 64 characters.",
  invalid_range: "That time range is not valid. Pick another range.",
  invalid_interval: "That chart interval is not supported. Pick another one.",
  // Comments, votes, reports and profiles
  comment_not_found: "That comment is no longer available.",
  not_holder: "Only holders and the creator of this token can do this.",
  own_comment: "You cannot do this on your own comment.",
  invalid_comment: "This comment breaks a posting rule. Check its length and content.",
  invalid_vote: "Choose up or down.",
  invalid_report: "Choose a report reason.",
  invalid_username: "Usernames are 3-20 characters of a-z, 0-9 and _.",
  username_taken: "That username is taken.",
  username_reserved: "That username is reserved.",
  invalid_wallet: "Each published wallet must be a full 0x address.",
  too_many_public_wallets: "You can publish at most 5 wallets.",
  // Watchlist and notifications
  watchlist_full: "Your watchlist is full. Remove a token before adding another.",
  invalid_notification_id: "That notification is no longer available. Reload your notifications.",
  telegram_not_linked: "Telegram is not connected any more. Connect it again.",
  telegram_unavailable: "Telegram alerts are not available on this deployment.",
  invalid_threshold: "Enter a trade size from 0.5 to 10,000 ETH.",
  // Token details and images
  invalid_metadata:
    "Check the details: the description can be at most 2,000 bytes, the X link must point at x.com or twitter.com, and the Telegram link at t.me or telegram.me.",
  invalid_image: "Use a PNG, JPEG or WebP image up to 5 MiB and 4096 x 4096 pixels.",
  image_type_mismatch:
    "The file's contents do not match its type. Export it again as PNG, JPEG or WebP.",
  invalid_revision: "This form is out of date. Reload the page and try again.",
  revision_conflict: "This record changed. Review the latest version before saving.",
  // Read features that a deployment may not offer
  early_buyers_unavailable: "Early-buyer data is not available on this deployment.",
  stats_unavailable: "Protocol statistics are not available right now. Try again shortly.",
  // Generic codes the API derives from the HTTP status (apiserver.problemCodeForStatus)
  bad_request: "The request was not valid. Reload the page and try again.",
  not_found: "That item was not found.",
  method_not_allowed: "This action is not supported. Reload the page and try again.",
  not_acceptable: "This response format is not supported. Reload the page and try again.",
  conflict: "This changed while you were working. Reload the page and try again.",
  payload_too_large: "The upload is too large. Use a smaller file.",
  unsupported_media_type: "That file type is not supported.",
  invalid_request: "Some values were not accepted. Check the form and try again.",
  rate_limited: "Too many requests. Wait a moment and try again.",
  internal_error: "Something failed on our side. Try again in a moment.",
  service_unavailable: "This feature is temporarily unavailable. Try again shortly.",
  timeout: "The server took too long to answer. Try again.",
  not_ready: "The service is starting up. Try again in a moment.",
  origin_not_allowed: "This site is not allowed to use the API. Open the app from its official address.",
  // Client-side transport failures (api/transport.ts)
  network_unreachable: "Could not reach the server. Check your connection and try again.",
};

/** Fallback for an API code that has no entry (an older client talking to a newer API). */
export const apiFallbackMessage = "The request could not be completed. Try again.";

/** API codes whose message carries a request reference, because only support can act on them. */
export const apiReferenceCodes: ReadonlySet<string> = new Set([
  "internal_error",
  "service_unavailable",
  "timeout",
  "not_ready",
  "bad_request",
  "invalid_request",
  "conflict",
  "method_not_allowed",
  "not_acceptable",
  "origin_not_allowed",
  "quote_failed",
]);

/** Where an error is shown; a context may word a code differently. */
export type ErrorContext =
  | "comment"
  | "vote"
  | "report"
  | "profile"
  | "telegram"
  | "metadata"
  | "launch-details"
  | "watchlist";

/** Context-specific wording; anything not listed reads as in `apiMessages`. */
export const contextMessages: Readonly<
  Record<ErrorContext, Readonly<Record<string, string>> & { fallback?: string }>
> = {
  comment: {
    wallet_not_linked: "That wallet is not linked to your account.",
    token_not_found: "This token is no longer available.",
    invalid_comment: "This comment breaks a posting rule.",
    not_holder: "Only holders and the creator of this token can comment.",
    banned: "Your account cannot post or report comments.",
    rate_limited: "You are posting too fast. Try again shortly.",
    invalid_cursor: "The comment list changed. Refresh to continue.",
    invalid_limit: "The comment list request was invalid.",
    own_comment: "You cannot report your own comment.",
    fallback: "Something went wrong. Try again.",
  },
  vote: {
    not_holder: "Hold this token to vote on its comments.",
    own_comment: "You cannot vote on your own comment.",
    rate_limited: "You are voting too fast. Try again shortly.",
    banned: "Your account cannot vote on comments.",
    fallback: "Your vote was not saved. Try again.",
  },
  report: {
    own_comment: "You cannot report your own comment.",
    rate_limited: "You are reporting too fast. Try again shortly.",
    fallback: "The report was not sent. Try again.",
  },
  profile: {
    rate_limited: "Your username was changed recently. Try again later.",
  },
  telegram: {
    fallback: "Your Telegram settings could not be saved. Try again shortly.",
  },
  metadata: {
    creator_required:
      "Only the launching wallet can change these details. Sign in with the wallet that launched this token.",
    rate_limited: "Too many writes in a minute. Wait a moment, then retry.",
    fallback: "The metadata update was rejected. Try again.",
  },
  "launch-details": {
    creator_required:
      "The signed-in account does not own the launching wallet, so the server refused the details.",
    authentication_failed: "Sign in again, then retry.",
    rate_limited: "Too many writes in a minute. Wait a moment, then retry.",
  },
  watchlist: {
    authentication_failed: "Sign in again to change your watchlist.",
    fallback: "Your watchlist could not be updated. Try again.",
  },
};

/**
 * Custom errors of the reviewed contracts and the Uniswap contracts the app calls. Users can
 * reach the first group by trading; the rest only signal a broken or misconfigured deployment,
 * so they say that plainly instead of asking the user to fix something they cannot.
 */
const deploymentFault =
  "The reviewed contracts refused this action because of a deployment fault. Nothing was changed; report it to the operators.";

export const contractMessages: Readonly<Record<string, string>> = {
  // Trading and launching
  DeadlineExpired: "The deadline expired. Refresh the quote and try again.",
  TransactionDeadlinePassed: "The deadline expired. Refresh the quote and try again.",
  SlippageExceeded:
    "Price moved beyond your slippage limit. Increase the limit only if you accept the risk.",
  V4TooLittleReceived:
    "Price moved beyond your slippage limit. Increase the limit only if you accept the risk.",
  QuoteChanged:
    "The on-chain quote changed before signing. Refresh and review the new minimum output.",
  Oversell: "The requested token amount exceeds the curve balance.",
  WrongPhase: "This token changed phase. Refresh the token before trading.",
  AlreadyGraduated: "This token has already graduated. Refresh the page to trade it on the pool.",
  TradingPaused: "Buying is paused by the reviewed deployment. Selling remains available.",
  LaunchesPaused: "Launching is paused by the reviewed deployment.",
  LaunchValueMismatch: "The launch value changed. Refresh the factory fee and try again.",
  DeveloperBuyCapExceeded: "The developer buy exceeds the contract cap. Reduce the amount and retry.",
  UnknownEngine: "This launch engine is not supported by the reviewed factory.",
  EngineDisabled: "This launch engine is disabled by the reviewed factory.",
  EngineVersionMismatch: "The selected engine version does not match the reviewed implementation.",
  ZeroInput: "Enter an amount greater than zero.",
  ZeroOutput: "This amount is too small to receive anything. Increase it.",
  ZeroAddress: "A required address is empty. Reconnect your wallet and try again.",
  InvalidRecipient: "Tokens cannot be sent to the curve or its liquidity pair.",
  TransferRestricted:
    "This token cannot be sent to a pool before it graduates. Trade it on the curve instead.",
  GraduationOverspent: "This buy would overshoot graduation. Lower the amount and retry.",
  GraduationPriceOutOfRange:
    "Graduation cannot open the pool at this price right now. Refresh the token and try again.",
  AntiSnipeBpsTooHigh:
    "Launch protection limits buys this early. Wait a few blocks or buy a smaller amount.",
  NotEnoughLiquidity: "The pool does not have enough liquidity for this amount. Lower it.",
  // Claims
  NothingToClaim: "There is no claimable balance for this wallet.",
  UnauthorizedCreatorClaim: "Only the linked creator wallet can claim these fees.",
  UnauthorizedProtocolClaim: "Only the protocol treasury can claim these fees.",
  UnauthorizedFeeRecipient: "Only the fee recipient wallet can do this.",
  UnauthorizedPendingFeeRecipient: "Only the proposed fee recipient wallet can accept this.",
  PositionNotOwned: "This wallet does not own that liquidity position.",
  // Balances and approvals
  ERC20InsufficientAllowance:
    "Approval is lower than this sell amount. Approve the disclosed amount and resume.",
  InsufficientAllowance:
    "Approval is lower than this amount. Approve the disclosed amount and resume.",
  AllowanceExpired: "The token approval expired. Approve again, then retry.",
  ERC20InsufficientBalance: "The selected wallet does not have enough tokens for this action.",
  InsufficientBalance: "The selected wallet does not have enough ETH for this action.",
  InsufficientETHBalance: "The selected wallet does not have enough ETH for this action.",
  ERC20InvalidReceiver: "Tokens cannot be sent to that address.",
  ERC20InvalidSender: "Tokens cannot be sent from that address.",
  ERC20InvalidApprover: "That address cannot approve tokens.",
  ERC20InvalidSpender: "That address cannot be approved to spend tokens.",
  // Transfers that failed inside the contract
  EthTransferFailed:
    "The contract could not send ETH to the recipient. Use a wallet that accepts ETH and retry.",
  WethTransferFailed: "A wrapped-ETH transfer failed inside the contract. Try again shortly.",
  TokenTransferFailed: "A token transfer failed inside the contract. Try again shortly.",
  SafeERC20FailedOperation: "A token transfer failed inside the contract. Try again shortly.",
  UnexpectedEthSender: "The contract refused ETH from this sender.",
  ReentrancyGuardReentrantCall: "The contract refused a nested call. Try again without batching.",
  ExecutionFailed: "One step of the swap failed. Refresh the quote and try again.",
  LengthMismatch: "The swap was built incorrectly. Refresh the page and try again.",
  UnexpectedRevertBytes: "The pool quote failed. Refresh the quote and try again.",
  ReceiptReverted: "The transaction was mined but reverted. No state change was accepted.",
  // Deployment faults: the user cannot fix these
  AccountingInvariantFailed: deploymentFault,
  CurveInvariantFailed: deploymentFault,
  ArithmeticOverflow: deploymentFault,
  DivisionByZero: deploymentFault,
  SafeCastOverflow: deploymentFault,
  AlreadyInitialized: deploymentFault,
  NotInitialized: deploymentFault,
  ImplementationInitializationDisabled: deploymentFault,
  DependenciesAlreadySet: deploymentFault,
  DependenciesNotSet: deploymentFault,
  InvalidDependency: deploymentFault,
  InvalidEngineImplementation: deploymentFault,
  FailedDeployment: deploymentFault,
  InvalidAuthority: deploymentFault,
  InvalidAntiSnipeProfile: deploymentFault,
  InvalidCurveAllocation: deploymentFault,
  InvalidCurveBoundary: deploymentFault,
  InvalidGraduationEth: deploymentFault,
  InvalidLpCreatorShareBps: deploymentFault,
  InvalidProtocolShareBps: deploymentFault,
  InvalidSupplyAllocation: deploymentFault,
  InvalidTradeFeeBps: deploymentFault,
  InvalidVirtualReserves: deploymentFault,
  InvalidPoolKey: deploymentFault,
  PairFactoryMismatch: deploymentFault,
  PairLiquidityZero: deploymentFault,
  PairNotCanonical: deploymentFault,
  PairSupplyNotZero: deploymentFault,
  PairTokenBalanceNotZero: deploymentFault,
  PairTokenReserveNotZero: deploymentFault,
  PairTokensMismatch: deploymentFault,
  PositionAlreadyRegistered: deploymentFault,
  PositionTokenMismatch: deploymentFault,
  NotPoolManager: deploymentFault,
  NotPositionManager: deploymentFault,
  HookNotImplemented: deploymentFault,
  UnknownToken: deploymentFault,
  UnauthorizedCurve: deploymentFault,
  UnauthorizedDeployer: deploymentFault,
  UnauthorizedFactory: deploymentFault,
  UnauthorizedInitializer: deploymentFault,
  UnauthorizedPauseAuthority: deploymentFault,
  UnauthorizedRegistrar: deploymentFault,
  UnauthorizedTimelock: deploymentFault,
};

/**
 * Revert reason strings of the Uniswap v2 router, library, pair and TransferHelper, which use
 * `require` strings instead of custom errors. Matched as substrings, most specific first.
 */
export const revertReasonMessages: ReadonlyArray<readonly [reason: string, message: string]> = [
  [
    "INSUFFICIENT_OUTPUT_AMOUNT",
    "Price moved beyond your slippage limit. Increase the limit only if you accept the risk.",
  ],
  [
    "EXCESSIVE_INPUT_AMOUNT",
    "Price moved beyond your slippage limit. Increase the limit only if you accept the risk.",
  ],
  ["UniswapV2Router: EXPIRED", "The deadline expired. Refresh the quote and try again."],
  ["INSUFFICIENT_LIQUIDITY", "The pool does not have enough liquidity for this amount. Lower it."],
  ["INSUFFICIENT_INPUT_AMOUNT", "Enter an amount greater than zero."],
  ["INSUFFICIENT_A_AMOUNT", "Price moved beyond your slippage limit. Refresh and try again."],
  ["INSUFFICIENT_B_AMOUNT", "Price moved beyond your slippage limit. Refresh and try again."],
  ["INVALID_PATH", "The swap route is not valid. Refresh the page and try again."],
  [
    "TRANSFER_FROM_FAILED",
    "The token transfer failed. Check the approval and your balance, then retry.",
  ],
  ["ETH_TRANSFER_FAILED", "The pool could not send ETH to this wallet. Try again shortly."],
  ["TRANSFER_FAILED", "A token transfer failed inside the pool. Try again shortly."],
  ["UniswapV2: K", "The pool refused this trade. Refresh the quote and try again."],
  ["UniswapV2: LOCKED", "The pool is busy. Try again in a moment."],
];

/** Solidity `Panic(uint256)` codes. */
export const panicMessages: Readonly<Record<number, string>> = {
  0x01: deploymentFault,
  0x11: "The amount is too large for the contract to process. Lower it.",
  0x12: deploymentFault,
  0x21: deploymentFault,
  0x22: deploymentFault,
  0x31: deploymentFault,
  0x32: deploymentFault,
  0x41: "The contract ran out of memory for this amount. Lower it.",
  0x51: deploymentFault,
};

/** Wallet, RPC and network failure kinds, recognised from EIP-1193 codes and viem error names. */
export const walletMessages = {
  "user-rejected": "You rejected the request in your wallet. Nothing was sent.",
  "wallet-busy": "Your wallet already has a request open. Finish or close it, then try again.",
  "wallet-unauthorized": "Your wallet did not allow this site. Connect the wallet again.",
  "wallet-unsupported": "Your wallet does not support this action. Try another wallet.",
  "wallet-disconnected": "Your wallet disconnected. Reconnect it and try again.",
  "chain-mismatch": "Your wallet is on another network. Switch to the reviewed network and retry.",
  "chain-not-added": "Your wallet does not know this network yet. Add it in the wallet, then retry.",
  "switch-failed": "The wallet did not switch networks. Switch it manually, then retry.",
  "insufficient-funds": "The selected wallet does not have enough ETH for the amount plus gas.",
  "nonce-too-low":
    "Your wallet has a newer transaction already. Wait for it to confirm, then retry.",
  "nonce-too-high": "Your wallet's transaction count is out of sync. Reset it in the wallet, then retry.",
  "replacement-underpriced":
    "A pending transaction with the same nonce pays a higher fee. Wait for it or speed it up.",
  "fee-too-low": "The network fee is too low right now. Retry so the wallet picks a new fee.",
  "fee-too-high": "The network fee the wallet chose is too high. Retry, or set the fee in the wallet.",
  "gas-too-low": "The wallet set too little gas. Retry and let the wallet estimate gas.",
  "gas-too-high": "This transaction needs more gas than a block allows. Lower the amount.",
  "rpc-rate-limited": "The network provider is busy. Wait a moment and try again.",
  "rpc-unavailable": "The network provider is unavailable. Try again shortly.",
  "rpc-timeout": "The network did not answer in time. Try again.",
  "rpc-unreachable": "Could not reach the network. Check your connection and try again.",
  "execution-reverted":
    "The contract refused this transaction. Refresh the token and review the amount before retrying.",
  "outcome-unknown":
    "The transaction was sent, but its result is not known yet. It may still confirm; check the hash in an explorer before sending it again.",
  unknown: "The wallet or network could not complete this request. Nothing was sent; try again.",
} as const;

export type WalletFailureKind = keyof typeof walletMessages;
