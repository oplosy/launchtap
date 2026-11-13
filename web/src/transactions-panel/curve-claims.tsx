"use client";

import { useEffect, useRef, useState } from "react";
import type { Abi, WalletClient } from "viem";
import type { useAccount, usePublicClient } from "wagmi";
import type { publicConfiguration } from "@/config/public";
import type { WalletReadiness } from "@/wallet/readiness";
import { classifyTransactionFailure, decodeTransactionError } from "@/transactions";
import { Badge, Button } from "@/components/primitives";
import {
  type Address,
  ErrorCopy,
  eth,
  hasUnresolvedSubmission,
  statusLabel,
  usePersistedTransactionState,
} from "./shared";

/**
 * Creator-fee and refund claims on the bonding curve: the claimable balances, both claim
 * transactions and their execution. TradingPanelReady calls it on every render, whichever
 * branch it renders, so claim state survives a phase change.
 */
export function useCurveClaims({
  configuration,
  account,
  readiness,
  chainId,
  publicClient,
  walletClient,
  tokenAddress,
  curveAddress,
  safeCurveAddress,
  curveAbi,
  engineVersion,
  graduated,
}: {
  configuration: ReturnType<typeof publicConfiguration>;
  account: ReturnType<typeof useAccount>;
  readiness: WalletReadiness;
  chainId: WalletReadiness["chainId"];
  publicClient: ReturnType<typeof usePublicClient>;
  walletClient: WalletClient | undefined;
  tokenAddress: Address | null;
  curveAddress: Address | null;
  safeCurveAddress: Address;
  curveAbi: Abi;
  engineVersion: number;
  graduated: boolean;
}) {
  const [creatorClaimable, setCreatorClaimable] = useState<bigint | null>(null);
  const [refundClaimable, setRefundClaimable] = useState<bigint | null>(null);
  const [claimBusy, setClaimBusy] = useState(false);
  const claimLockRef = useRef(false);
  // Engine-2 curve claims stay open after graduation (see claimRow); engine 1 is unchanged.
  const claimsOpen = !graduated || engineVersion === 2;
  const creatorClaimTransaction = usePersistedTransactionState(
    "claim",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
    claimsOpen,
  );
  const refundClaimTransaction = usePersistedTransactionState(
    "refund",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
    claimsOpen,
  );
  const setCreatorClaimState = creatorClaimTransaction.setState;
  const setRefundClaimState = refundClaimTransaction.setState;

  useEffect(() => {
    let cancelled = false;
    if (!publicClient || !account.address || !curveAddress) return;
    void Promise.all([
      publicClient.readContract({
        address: safeCurveAddress,
        abi: curveAbi,
        functionName: "creator",
      }),
      // Engine 2 credits curve creator fees per account to the fee recipient in force at
      // credit time, so the connected account reads its own balance (spec §5.3).
      engineVersion === 2
        ? publicClient.readContract({
            address: safeCurveAddress,
            abi: curveAbi,
            functionName: "creatorFeesOwed",
            args: [account.address],
          })
        : publicClient.readContract({
            address: safeCurveAddress,
            abi: curveAbi,
            functionName: "unclaimedCreatorFees",
          }),
      publicClient.readContract({
        address: safeCurveAddress,
        abi: curveAbi,
        functionName: "pendingRefund",
        args: [account.address],
      }),
    ])
      .then(([creator, creatorFees, refund]) => {
        if (cancelled) return;
        setCreatorClaimable(
          engineVersion === 2 || String(creator).toLowerCase() === account.address!.toLowerCase()
            ? (creatorFees as bigint)
            : 0n,
        );
        setRefundClaimable(refund as bigint);
      })
      .catch(() => {
        if (!cancelled) {
          setCreatorClaimable(null);
          setRefundClaimable(null);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [account.address, curveAbi, curveAddress, engineVersion, publicClient, safeCurveAddress]);

  const executeClaim = async (kind: "creator" | "refund") => {
    const claimTransaction = kind === "creator" ? creatorClaimTransaction : refundClaimTransaction;
    const claimState = claimTransaction.state;
    if (
      claimLockRef.current ||
      !claimTransaction.ready ||
      hasUnresolvedSubmission(claimState) ||
      !publicClient ||
      !walletClient ||
      !account.address ||
      chainId !== configuration.chainId ||
      !readiness.selectedAccountVerified
    )
      return;
    claimLockRef.current = true;
    setClaimBusy(true);
    const setClaimState = kind === "creator" ? setCreatorClaimState : setRefundClaimState;
    let submittedHash: Address | undefined;
    let failurePhase: "preflight" | "simulation" | "write" | "receipt" = "preflight";
    try {
      setClaimState({ status: "validating" });
      const [walletAccounts, latestChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        latestChainId !== configuration.chainId ||
        walletAccounts[0]?.toLowerCase() !== account.address.toLowerCase()
      )
        throw new Error("Wallet or network changed; review the claim again.");
      if (kind === "creator" && engineVersion === 2) {
        const amount = await publicClient.readContract({
          address: safeCurveAddress,
          abi: curveAbi,
          functionName: "creatorFeesOwed",
          args: [account.address],
        });
        if ((amount as bigint) === 0n) throw new Error("NothingToClaim");
      } else if (kind === "creator") {
        const [creator, amount] = await Promise.all([
          publicClient.readContract({
            address: safeCurveAddress,
            abi: curveAbi,
            functionName: "creator",
          }),
          publicClient.readContract({
            address: safeCurveAddress,
            abi: curveAbi,
            functionName: "unclaimedCreatorFees",
          }),
        ]);
        if (
          String(creator).toLowerCase() !== account.address.toLowerCase() ||
          (amount as bigint) === 0n
        )
          throw new Error("NothingToClaim");
      } else {
        const amount = await publicClient.readContract({
          address: safeCurveAddress,
          abi: curveAbi,
          functionName: "pendingRefund",
          args: [account.address],
        });
        if ((amount as bigint) === 0n) throw new Error("NothingToClaim");
      }
      const functionName = kind === "creator" ? "claimCreatorFees" : "claimRefund";
      setClaimState({ status: "simulating" });
      failurePhase = "simulation";
      await publicClient.simulateContract({
        address: safeCurveAddress,
        abi: curveAbi,
        functionName,
        account: account.address,
      } as never);
      setClaimState({ status: "awaiting-signature" });
      failurePhase = "write";
      const hash = await walletClient.writeContract({
        address: safeCurveAddress,
        abi: curveAbi,
        functionName,
        account: account.address,
      } as never);
      submittedHash = hash as Address;
      setClaimState({ status: "submitted", hash: submittedHash });
      failurePhase = "receipt";
      const receipt = await publicClient.waitForTransactionReceipt({ hash });
      if (receipt.status !== "success") throw new Error("ReceiptReverted");
      setClaimState({ status: "indexing", hash: submittedHash });
    } catch (cause) {
      setClaimState({
        status: classifyTransactionFailure(cause, failurePhase),
        hash: submittedHash,
        error: decodeTransactionError(cause).message,
      });
    } finally {
      claimLockRef.current = false;
      setClaimBusy(false);
    }
  };
  return {
    creatorClaimable,
    refundClaimable,
    claimBusy,
    creatorClaimTransaction,
    refundClaimTransaction,
    executeClaim,
  };
}

export type CurveClaims = ReturnType<typeof useCurveClaims>;

// Curve claims stay open after graduation: engine-2 curve creator fees accrue per account
// and remain claimable on the curve once the token trades in its V4 pool (spec §5.3).
export function CurveClaimRow({ claims }: { claims: CurveClaims }) {
  const {
    creatorClaimable,
    refundClaimable,
    claimBusy,
    creatorClaimTransaction,
    refundClaimTransaction,
    executeClaim,
  } = claims;
  const creatorClaimState = creatorClaimTransaction.state;
  const refundClaimState = refundClaimTransaction.state;
  return (
    <>
      <div className="claim-row">
        <span>
          Claimable creator fees:{" "}
          {creatorClaimable === null ? "Unavailable" : eth(creatorClaimable)}
          <br />
          Claimable refund: {refundClaimable === null ? "Unavailable" : eth(refundClaimable)}
        </span>
        <div className="transaction-actions">
          <Button
            size="sm"
            disabled={
              creatorClaimable === null ||
              creatorClaimable === 0n ||
              claimBusy ||
              !creatorClaimTransaction.ready ||
              hasUnresolvedSubmission(creatorClaimState)
            }
            loading={
              creatorClaimState.status === "simulating" ||
              creatorClaimState.status === "awaiting-signature"
            }
            onClick={() => void executeClaim("creator")}
          >
            Claim creator fees
          </Button>
          <Button
            size="sm"
            disabled={
              refundClaimable === null ||
              refundClaimable === 0n ||
              claimBusy ||
              !refundClaimTransaction.ready ||
              hasUnresolvedSubmission(refundClaimState)
            }
            loading={
              refundClaimState.status === "simulating" ||
              refundClaimState.status === "awaiting-signature"
            }
            onClick={() => void executeClaim("refund")}
          >
            Claim refund
          </Button>
        </div>
      </div>
      {[creatorClaimState, refundClaimState].map((claim) =>
        claim.status !== "disconnected" ? (
          <div className="transaction-progress" role="status" key={claim.hash ?? claim.status}>
            <Badge
              tone={
                claim.status === "reverted" || claim.status === "rejected-signature"
                  ? "danger"
                  : "warning"
              }
            >
              {statusLabel(claim.status)}
            </Badge>
            {claim.hash ? <span className="mono">{claim.hash}</span> : null}
            {claim.error ? <ErrorCopy error={new Error(claim.error)} /> : null}
            {claim.canonicalRefreshUnavailable ? (
              <span className="transaction-error" role="alert">
                Canonical status refresh unavailable. Showing the last known status.
              </span>
            ) : null}
          </div>
        ) : null,
      )}
    </>
  );
}
