"use client";

import { Button } from "@/components/primitives";
import { ConnectOr, ReviewActions, ReviewNotice, useReviewedTrade } from "./reviewed-trade";
import { CurveTradeDetails, type CurveReviewDetails } from "./curve-trade-views";
import type { TradingToken } from "./curve-trade-logic";

/** The curve trade's review step: the "Review" button, then the confirmation with "Sign". */
export function CurveReviewStep({
  trade,
  token,
  reviewDetails,
  curveExplorer,
  canTrade,
  canSubmit,
  onReview,
  onSign,
}: {
  trade: ReturnType<typeof useReviewedTrade>;
  token: TradingToken;
  reviewDetails: CurveReviewDetails | null;
  curveExplorer: string | null;
  canTrade: boolean;
  canSubmit: boolean;
  onReview: () => void;
  onSign: () => void;
}) {
  const {
    side,
    input,
    slippage,
    slippageBps,
    reviewIntent,
    reviewNotice,
    configuration,
    confirming,
    executionBusy,
    transaction,
    approvalTransaction,
    approvalHash,
  } = trade;
  return confirming ? (
    <div className="transaction-confirmation" role="region" aria-label="Trade confirmation">
      <h3>Confirm {reviewDetails?.side ?? side}</h3>
      <ReviewNotice notice={reviewNotice} />
      <CurveTradeDetails
        token={token}
        side={side}
        input={input}
        reviewDetails={reviewDetails}
        reviewIntent={reviewIntent}
        slippage={slippage}
        slippageBps={slippageBps}
        configuration={configuration}
        curveExplorer={curveExplorer}
      />
      <ReviewActions write={trade} label={`Sign ${side}`} canSign={canSubmit} onSign={onSign} />
    </div>
  ) : (
    <>
      <ReviewNotice notice={reviewNotice} />
      <ConnectOr write={trade}>
        <Button
          variant="primary"
          size="lg"
          disabled={
            executionBusy ||
            !canTrade ||
            !transaction.ready ||
            (side === "sell" && !approvalTransaction.ready) ||
            trade.unresolved
          }
          onClick={onReview}
        >
          {side === "sell" && approvalHash ? "Resume sell" : `Review ${side}`}
        </Button>
      </ConnectOr>
    </>
  );
}
