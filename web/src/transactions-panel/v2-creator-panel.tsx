"use client";

import { useEffect, useRef, useState } from "react";
import { useAccount, usePublicClient, useWalletClient } from "wagmi";
import { formatBaseUnits } from "@/amounts";
import type { Abi } from "viem";
import { recipientActions, validateProposedRecipient } from "@/fee-recipient";
import { browserAbis } from "@/contracts/generated";
import { publicConfiguration } from "@/config/public";
import { useWalletReadiness } from "@/wallet/readiness";
import { classifyTransactionFailure, decodeTransactionError } from "@/transactions";
import { Button, Input } from "@/components/primitives";
import { type Address, eth, usePersistedTransactionState, TransactionProgress } from "./shared";

/**
 * Engine-2 creator surface (Plan v2-C Task 6, spec §5.3): the creator (identity) and the fee
 * recipient (payout) as separate facts, the two-step recipient change, and the LP fee locker
 * claim. Every balance and role is read on-chain for the connected account; the offered
 * actions mirror the curve's access checks (`recipientActions`).
 */
export function V2CreatorPanel({
  tokenAddress,
  curveAddress,
  locker,
  graduated,
}: {
  tokenAddress: Address;
  curveAddress: Address;
  locker: Address | null;
  graduated: boolean;
}) {
  const account = useAccount();
  const readiness = useWalletReadiness();
  const publicClient = usePublicClient();
  const { data: walletClient } = useWalletClient();
  const configuration = publicConfiguration();
  const [roles, setRoles] = useState<{
    creator: string;
    feeRecipient: string;
    pendingFeeRecipient: string;
  } | null>(null);
  const [lockerOwed, setLockerOwed] = useState<{ eth: bigint; token: bigint } | null>(null);
  const [refresh, setRefresh] = useState(0);
  const [proposal, setProposal] = useState("");
  const [busy, setBusy] = useState(false);
  // Persisted so a reload mid-transaction keeps the hash and the lock (Plan v2-C Task 9).
  const creatorTransaction = usePersistedTransactionState(
    "v2-creator",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
  );
  const { state, setState, ready } = creatorTransaction;
  const lockRef = useRef(false);
  const curveAbi = browserAbis.curveV2 as Abi;
  // A restored hash whose outcome is unknown settles on its receipt; no indexer event follows.
  useEffect(() => {
    if (!ready || !publicClient || lockRef.current || !state.hash) return;
    if (state.status !== "submitted" && state.status !== "rpc-failure") return;
    const hash = state.hash as Address;
    let cancelled = false;
    void publicClient
      .waitForTransactionReceipt({ hash })
      .then((receipt) => {
        if (cancelled) return;
        setState({ status: receipt.status === "success" ? "mined" : "receipt-reverted", hash });
        setRefresh((value) => value + 1);
      })
      .catch(() => {
        // The receipt stays unknown; the lock holds until the next attempt finds it.
      });
    return () => {
      cancelled = true;
    };
  }, [publicClient, ready, setState, state.hash, state.status]);
  useEffect(() => {
    if (!publicClient) return;
    let cancelled = false;
    void Promise.all([
      publicClient.readContract({ address: curveAddress, abi: curveAbi, functionName: "creator" }),
      publicClient.readContract({
        address: curveAddress,
        abi: curveAbi,
        functionName: "feeRecipient",
      }),
      publicClient.readContract({
        address: curveAddress,
        abi: curveAbi,
        functionName: "pendingFeeRecipient",
      }),
    ])
      .then(([creator, feeRecipient, pendingFeeRecipient]) => {
        if (!cancelled)
          setRoles({
            creator: String(creator),
            feeRecipient: String(feeRecipient),
            pendingFeeRecipient: String(pendingFeeRecipient),
          });
      })
      .catch(() => {
        if (!cancelled) setRoles(null);
      });
    return () => {
      cancelled = true;
    };
  }, [curveAbi, curveAddress, publicClient, refresh]);
  useEffect(() => {
    if (!publicClient || !locker || !account.address) return;
    let cancelled = false;
    void publicClient
      .readContract({
        address: locker,
        abi: browserAbis.locker,
        functionName: "owed",
        args: [tokenAddress, account.address],
      })
      .then((result) => {
        const [ethOwed, tokenOwed] = result as readonly [bigint, bigint];
        if (!cancelled) setLockerOwed({ eth: ethOwed, token: tokenOwed });
      })
      .catch(() => {
        if (!cancelled) setLockerOwed(null);
      });
    return () => {
      cancelled = true;
    };
  }, [account.address, locker, publicClient, refresh, tokenAddress]);
  const actions = roles
    ? recipientActions({
        account: account.address,
        feeRecipient: roles.feeRecipient,
        pendingFeeRecipient: roles.pendingFeeRecipient,
      })
    : [];
  const validation =
    roles && proposal.trim() ? validateProposedRecipient(proposal, roles.feeRecipient) : null;
  // These writes finish at "mined" (no indexing step follows), so a mined action is resolved;
  // only a hash whose outcome is still unknown blocks the next action.
  const outcomeUnknown = Boolean(
    state.hash && (state.status === "submitted" || state.status === "rpc-failure"),
  );
  const canSign = Boolean(
    publicClient &&
    walletClient &&
    account.address &&
    readiness.selectedAccountVerified &&
    readiness.chainId === configuration.chainId &&
    ready &&
    !outcomeUnknown,
  );
  const run = async (
    write: { address: Address; abi: Abi; functionName: string; args?: readonly unknown[] },
    checkSimulation?: (result: unknown) => void,
  ) => {
    if (lockRef.current || !canSign) return;
    lockRef.current = true;
    setBusy(true);
    let submittedHash: Address | undefined;
    let failurePhase: "preflight" | "simulation" | "write" | "receipt" = "preflight";
    try {
      setState({ status: "validating" });
      const [walletAccounts, latestChainId] = await Promise.all([
        walletClient!.getAddresses(),
        publicClient!.getChainId(),
      ]);
      if (
        latestChainId !== configuration.chainId ||
        walletAccounts[0]?.toLowerCase() !== account.address!.toLowerCase()
      )
        throw new Error("Wallet or network changed; review the action again.");
      setState({ status: "simulating" });
      failurePhase = "simulation";
      const { result } = await publicClient!.simulateContract({
        ...write,
        account: account.address,
      } as never);
      checkSimulation?.(result);
      setState({ status: "awaiting-signature" });
      failurePhase = "write";
      const hash = (await walletClient!.writeContract({
        ...write,
        account: account.address,
      } as never)) as Address;
      submittedHash = hash;
      setState({ status: "submitted", hash });
      failurePhase = "receipt";
      const receipt = await publicClient!.waitForTransactionReceipt({ hash });
      if (receipt.status !== "success") throw new Error("ReceiptReverted");
      setState({ status: "mined", hash });
      setProposal("");
      setRefresh((value) => value + 1);
    } catch (cause) {
      setState({
        status: classifyTransactionFailure(cause, failurePhase),
        hash: submittedHash,
        error: decodeTransactionError(cause).message,
      });
    } finally {
      lockRef.current = false;
      setBusy(false);
    }
  };
  const hasPending = roles !== null && !/^0x0{40}$/i.test(roles.pendingFeeRecipient);
  const proposeLabel = actions.includes("replace") ? "Replace proposal" : "Propose recipient";
  return (
    <div className="v2-creator">
      <h3>Creator fees</h3>
      <dl className="token-facts">
        <dt>Creator</dt>
        <dd className="mono">{roles?.creator ?? "Unavailable"}</dd>
        <dt>Fee recipient</dt>
        <dd className="mono">{roles?.feeRecipient ?? "Unavailable"}</dd>
        {hasPending ? (
          <>
            <dt>Pending recipient</dt>
            <dd className="mono">{roles!.pendingFeeRecipient}</dd>
          </>
        ) : null}
        {locker ? (
          <>
            <dt>Your LP fees</dt>
            <dd className="mono">
              {lockerOwed === null
                ? "Unavailable"
                : `${eth(lockerOwed.eth)} + ${formatBaseUnits(lockerOwed.token, 18, 6)} tokens`}
            </dd>
          </>
        ) : null}
      </dl>
      <small>
        The creator is the launch identity; the fee recipient receives new creator fees. A change
        takes effect only when the proposed address accepts it, and fees already credited stay with
        the account they were credited to.
      </small>
      {locker && graduated ? (
        <div className="transaction-actions">
          <Button
            size="sm"
            disabled={busy || !canSign}
            loading={state.status === "simulating" || state.status === "awaiting-signature"}
            onClick={() =>
              void run(
                {
                  address: locker,
                  abi: browserAbis.locker as Abi,
                  functionName: "claim",
                  args: [tokenAddress],
                },
                (result) => {
                  // `claim` collects pool fees first, so the simulation shows the full payout.
                  const [ethAmount, tokenAmount] = result as readonly [bigint, bigint];
                  if (ethAmount === 0n && tokenAmount === 0n) throw new Error("NothingToClaim");
                },
              )
            }
          >
            Collect and claim LP fees
          </Button>
        </div>
      ) : null}
      {actions.includes("propose") || actions.includes("replace") ? (
        <div className="transaction-form">
          <Input
            label="New fee recipient"
            value={proposal}
            disabled={busy}
            onChange={(event) => setProposal(event.target.value)}
            hint={
              validation && !validation.ok
                ? validation.error
                : "The new address must accept before it receives fees."
            }
          />
          <div className="transaction-actions">
            <Button
              size="sm"
              disabled={busy || !canSign || !validation?.ok}
              onClick={() =>
                validation?.ok
                  ? void run({
                      address: curveAddress,
                      abi: curveAbi,
                      functionName: "proposeFeeRecipient",
                      args: [validation.address],
                    })
                  : undefined
              }
            >
              {proposeLabel}
            </Button>
            {actions.includes("cancel") ? (
              <Button
                size="sm"
                variant="quiet"
                disabled={busy || !canSign}
                onClick={() =>
                  void run({
                    address: curveAddress,
                    abi: curveAbi,
                    functionName: "cancelFeeRecipientProposal",
                  })
                }
              >
                Cancel proposal
              </Button>
            ) : null}
          </div>
        </div>
      ) : null}
      {actions.includes("accept") ? (
        <div className="transaction-actions">
          <Button
            size="sm"
            disabled={busy || !canSign}
            onClick={() =>
              void run({
                address: curveAddress,
                abi: curveAbi,
                functionName: "acceptFeeRecipient",
              })
            }
          >
            Accept fee recipient role
          </Button>
        </div>
      ) : null}
      <TransactionProgress
        state={state}
        tone={state.status === "mined" ? "success" : "warning"}
        refreshNotice={false}
      />
    </div>
  );
}
