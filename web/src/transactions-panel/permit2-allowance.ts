import { browserAbis } from "@/contracts/generated";
import { sellSwapDeadline } from "@/v4-swap";
import type { Address } from "./shared";

type AllowanceReader = {
  readContract: (args: {
    address: Address;
    abi: typeof browserAbis.permit2;
    functionName: "allowance";
    args: readonly [Address, Address, Address];
  }) => Promise<unknown>;
};

/** The owner's Permit2 allowance of token to spender: amount and expiry (Unix seconds). */
export async function readPermit2Allowance(
  client: AllowanceReader,
  args: { permit2: Address; owner: Address; token: Address; spender: Address },
) {
  const [amount, expiration] = (await client.readContract({
    address: args.permit2,
    abi: browserAbis.permit2,
    functionName: "allowance",
    args: [args.owner, args.token, args.spender],
  })) as readonly [bigint, number | bigint, number | bigint];
  return { amount, expiration: BigInt(expiration) };
}

/** The deadline of a sell review, capped at the reusable Permit2 allowance (sellSwapDeadline). */
export async function sellReviewDeadline(
  client: AllowanceReader,
  args: { permit2: Address; owner: Address; token: Address; spender: Address } & {
    amount: bigint;
    nowSeconds: bigint;
    ttlSeconds: bigint;
  },
) {
  const permit2 = await readPermit2Allowance(client, args);
  return sellSwapDeadline({ ...args, permit2 });
}
