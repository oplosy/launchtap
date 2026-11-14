import type { Abi } from "viem";
import { snipeTaxLine } from "@/anti-snipe";
import { curvePriceImpact } from "@/price-impact";
import { minimumOutput, parseRuntimeQuantity, type ReviewedWriteIntent } from "@/transactions";
import { useTaxSecondsLeft, type Address } from "./shared";

/** The token fields the curve trading panel reads. */
export type TradingToken = {
  address: string;
  curve: string;
  pair: string;
  phase: string;
  name: string;
  symbol: string;
  engineVersion?: number;
  antiSnipeEndsAt?: string | null;
  v2PoolId?: string | null;
  /** Last indexed spot price, wei per whole token; only used for the price-impact estimate. */
  spotPriceWei?: string | null;
};

type CurveReader = {
  readContract: (args: {
    address: Address;
    abi: Abi;
    functionName: string;
    args?: readonly unknown[];
  }) => Promise<unknown>;
};

/**
 * Reads a fresh contract quote and builds the exact write intent for one curve buy or sell.
 * Engine 2 buys also return the anti-snipe tax (fifth quote value) and the current tax rate;
 * sells are untaxed. The minimum output is computed from the contract quote, not the API quote.
 */
export async function readCurveTradeIntent(args: {
  publicClient: CurveReader;
  curve: Address;
  abi: Abi;
  engineVersion: number;
  chainId: number | null;
  account: Address;
  side: "buy" | "sell";
  quantity: bigint;
  toleranceBps: bigint;
  deadline: bigint;
}) {
  const { publicClient, curve, abi, engineVersion, account, side, quantity, deadline } = args;
  const intentChainId = args.chainId;
  if (intentChainId === null) throw new Error("ChainUnavailable");
  const contractQuote = (await publicClient.readContract({
    address: curve,
    abi,
    functionName: side === "buy" ? "quoteBuy" : "quoteSell",
    args: [quantity],
  })) as readonly unknown[];
  const output =
    side === "buy"
      ? parseRuntimeQuantity(contractQuote[1], "contract output")
      : parseRuntimeQuantity(contractQuote[0], "contract output");
  const protocolFee = parseRuntimeQuantity(contractQuote[2], "protocol fee");
  const fee = protocolFee + parseRuntimeQuantity(contractQuote[3], "creator fee");
  let taxQuote = { snipeTax: 0n, taxBps: 0 };
  if (engineVersion === 2 && side === "buy") {
    const taxBps = await publicClient.readContract({
      address: curve,
      abi,
      functionName: "currentTaxBps",
    });
    taxQuote = {
      snipeTax: parseRuntimeQuantity(contractQuote[4], "snipe tax"),
      taxBps: Number(taxBps),
    };
  }
  const inputUsed =
    side === "buy" ? parseRuntimeQuantity(contractQuote[0], "contract input used") : quantity;
  const exactMinimum = minimumOutput(output, args.toleranceBps);
  const writeArgs =
    side === "buy"
      ? ([account, account, exactMinimum, deadline] as const)
      : ([quantity, account, exactMinimum, deadline] as const);
  return {
    intent: {
      account,
      target: curve,
      value: side === "buy" ? quantity : 0n,
      args: writeArgs,
      deadline,
      minimumOutput: exactMinimum,
      chainId: intentChainId,
      functionName: side,
    } satisfies ReviewedWriteIntent,
    output,
    fee,
    protocolFee,
    taxQuote,
    inputUsed,
  };
}

/**
 * Figures derived from the current quote: the anti-snipe tax line, the minimum output and the
 * price impact. Price impact is shown only for an untaxed quote: the tax is a separate charge,
 * not price movement.
 */
export function useCurveTradeFigures(args: {
  quote: { output: bigint; fee: bigint } | null;
  side: "buy" | "sell";
  slippageBps: bigint | null;
  taxQuote: { snipeTax: bigint; taxBps: number } | null;
  quoteInputUsed: bigint | null;
  antiSnipeEndsAt: string | null;
  spotPriceWei: string | null | undefined;
  publicClient: Parameters<typeof useTaxSecondsLeft>[1];
}) {
  const { quote, side, slippageBps, taxQuote, quoteInputUsed, spotPriceWei } = args;
  const taxSecondsLeft = useTaxSecondsLeft(args.antiSnipeEndsAt, args.publicClient);
  const taxLine =
    quote && side === "buy" && taxQuote
      ? snipeTaxLine({ ...taxQuote, remainingSeconds: taxSecondsLeft })
      : null;
  const minimum = quote && slippageBps !== null ? minimumOutput(quote.output, slippageBps) : null;
  const priceImpact =
    quote && quoteInputUsed !== null && taxQuote !== null && taxQuote.snipeTax === 0n
      ? curvePriceImpact({
          side,
          input: quoteInputUsed,
          output: quote.output,
          fee: quote.fee,
          spotPriceWad:
            spotPriceWei && /^[1-9][0-9]*$/.test(spotPriceWei) ? BigInt(spotPriceWei) : null,
        })
      : null;
  return { taxLine, minimum, priceImpact };
}
