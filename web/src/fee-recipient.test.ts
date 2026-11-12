import { getAddress } from "viem";
import { describe, expect, it } from "vitest";
import { recipientActions, validateProposedRecipient } from "./fee-recipient";

const creator = "0x00000000000000000000000000000000000000c1";
const recipient = "0x00000000000000000000000000000000000000a1";
const pending = "0x00000000000000000000000000000000000000b1";
const stranger = "0x00000000000000000000000000000000000000d1";
const ZERO = "0x0000000000000000000000000000000000000000";

describe("fee recipient action matrix", () => {
  const cases: {
    role: string;
    account: string;
    noProposal: string[];
    withProposal: string[];
  }[] = [
    // The creator is identity only (spec §5.3): once the recipient moved away, it has no power.
    { role: "creator", account: creator, noProposal: [], withProposal: [] },
    {
      role: "current recipient",
      account: recipient,
      noProposal: ["propose"],
      withProposal: ["replace", "cancel"],
    },
    { role: "pending recipient", account: pending, noProposal: [], withProposal: ["accept"] },
    { role: "stranger", account: stranger, noProposal: [], withProposal: [] },
  ];

  for (const { role, account, noProposal, withProposal } of cases) {
    it(`offers ${role} the right actions with and without a pending proposal`, () => {
      expect(
        recipientActions({ account, feeRecipient: recipient, pendingFeeRecipient: ZERO }),
      ).toEqual(noProposal);
      expect(
        recipientActions({ account, feeRecipient: recipient, pendingFeeRecipient: pending }),
      ).toEqual(withProposal);
    });
  }

  it("compares addresses case-insensitively and offers nothing without an account", () => {
    expect(
      recipientActions({
        account: recipient.toUpperCase().replace("0X", "0x"),
        feeRecipient: recipient,
        pendingFeeRecipient: ZERO,
      }),
    ).toEqual(["propose"]);
    expect(
      recipientActions({ account: undefined, feeRecipient: recipient, pendingFeeRecipient: ZERO }),
    ).toEqual([]);
  });

  it("lets the creator act while it is still the recipient", () => {
    expect(
      recipientActions({ account: creator, feeRecipient: creator, pendingFeeRecipient: ZERO }),
    ).toEqual(["propose"]);
  });
});

describe("proposed recipient validation", () => {
  it("returns the checksummed address for a valid proposal", () => {
    expect(
      validateProposedRecipient(" 0x00000000000000000000000000000000000000b1 ", recipient),
    ).toEqual({ ok: true, address: getAddress(pending) });
  });

  it("rejects malformed, bad-checksum, zero and unchanged addresses", () => {
    expect(validateProposedRecipient("0x1234", recipient)).toMatchObject({ ok: false });
    // Mixed case with a wrong checksum.
    expect(
      validateProposedRecipient("0xAbCdEf0000000000000000000000000000000000", recipient),
    ).toMatchObject({ ok: false });
    expect(validateProposedRecipient(ZERO, recipient)).toMatchObject({ ok: false });
    expect(
      validateProposedRecipient(recipient.toUpperCase().replace("0X", "0x"), recipient),
    ).toMatchObject({ ok: false });
  });
});
